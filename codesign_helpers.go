package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"aiko/internal/execenv"
)

// run executes an external command and waits for completion, merging stderr
// into the error message. cmd.Env uses execenv.AugmentedEnv() so that tools
// installed via Homebrew/npm/pipx are found even when launched from a .app bundle.
func run(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, stderr.String())
	}
	return nil
}

// ensureAikoCert makes sure a self-signed code-signing certificate named
// "Aiko" exists in the user's login keychain and is trusted for codesign.
// It creates the certificate via openssl and imports it using the security tool.
// Everything runs inside the user's keychain — no sudo required.
func ensureAikoCert() error {
	// Fast path: already present. Use `find-identity` without `-v` — the `-v`
	// flag drops self-signed certs (no trust chain), but codesign does not
	// require trust to use an identity, so untrusted identities are fine.
	out, _ := exec.Command("security", "find-identity", "-p", "codesigning").CombinedOutput()
	if strings.Contains(string(out), `"Aiko"`) {
		return nil
	}

	tmp, err := os.MkdirTemp("", "aiko-cert-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmp)

	keyPath := filepath.Join(tmp, "key.pem")
	cfgPath := filepath.Join(tmp, "openssl.cnf")
	crtPath := filepath.Join(tmp, "cert.pem")
	p12Path := filepath.Join(tmp, "cert.p12")

	// Minimal openssl config: CN=Aiko, codeSigning EKU.
	cfg := `[req]
distinguished_name = dn
prompt = no
x509_extensions = v3

[dn]
CN = Aiko

[v3]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("写入 openssl 配置失败: %w", err)
	}

	if err := run("openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048",
		"-keyout", keyPath, "-out", crtPath, "-days", "3650",
		"-config", cfgPath); err != nil {
		return fmt.Errorf("生成证书失败: %w", err)
	}

	// PKCS#12 bundle for keychain import. Forced to SHA1 MAC and legacy PBE
	// (PBE-SHA1-3DES) for compatibility with macOS `security import` —
	// modern OpenSSL defaults (SHA256 MAC, PBES2+AES) trigger
	// "MAC verification failed" on the security tool's parser. Password must
	// also be non-empty; the security tool mishandles empty-password p12.
	const p12Pass = "aiko"
	if err := run("openssl", "pkcs12", "-export",
		"-inkey", keyPath, "-in", crtPath,
		"-name", "Aiko", "-out", p12Path,
		"-passout", "pass:"+p12Pass,
		"-macalg", "SHA1",
		"-keypbe", "PBE-SHA1-3DES",
		"-certpbe", "PBE-SHA1-3DES"); err != nil {
		return fmt.Errorf("打包 PKCS#12 失败: %w", err)
	}

	// Resolve login keychain path (differs by OS version: ...db vs no-extension).
	loginKC := filepath.Join(os.Getenv("HOME"), "Library", "Keychains", "login.keychain-db")
	if _, err := os.Stat(loginKC); err != nil {
		loginKC = filepath.Join(os.Getenv("HOME"), "Library", "Keychains", "login.keychain")
	}

	// Import; -A allows any app to use the key without per-use prompt.
	if err := run("security", "import", p12Path, "-k", loginKC,
		"-P", p12Pass, "-T", "/usr/bin/codesign", "-A"); err != nil {
		return fmt.Errorf("导入证书失败: %w", err)
	}

	// Verify (without -v for self-signed cert, as above).
	out2, _ := exec.Command("security", "find-identity", "-p", "codesigning").CombinedOutput()
	if !strings.Contains(string(out2), `"Aiko"`) {
		return fmt.Errorf("证书导入后未被 codesign 识别")
	}
	return nil
}
