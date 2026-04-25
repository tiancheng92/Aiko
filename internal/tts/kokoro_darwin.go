//go:build darwin

package tts

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed scripts/kokoro_tts.py
var kokoroPyScript []byte

// KokoroSpeaker 通过调用内嵌的 kokoro_tts.py 实现 Speaker 接口。
// 使用 Kokoro-82M ONNX 模型 + misaki Chinese G2P，支持中日英多语言合成。
// 构造时不做路径校验，错误推迟到 Speak() 时报出。
type KokoroSpeaker struct {
	pythonBin  string // Python 可执行文件路径
	scriptPath string // kokoro_tts.py 路径（写入 ~/.aiko/）
	modelDir   string // 包含 kokoro-v1.0.onnx 和 voices-v1.0.bin 的目录
}

// kokoroVoices 是 Kokoro-82M 模型提供的中文声线列表（zf=女声，zm=男声）。
var kokoroVoices = []string{
	"zf_xiaobei",  // 小北（女，活泼）
	"zf_xiaoni",   // 小妮（女，温柔）
	"zf_xiaoxiao", // 小小（女，清新）
	"zf_xiaoyi",   // 小仪（女，知性）
}

// newKokoroSpeaker 是平台入口，由 tts.New 调用。
// modelDir 为 kokoro venv 根目录（含 bin/python3 和 models/），空串则使用默认 ~/.aiko/tts-venv。
// 构造时将内嵌脚本写入 ~/.aiko/kokoro_tts.py，确保路径始终可用。
func newKokoroSpeaker(modelDir string) (Speaker, error) {
	home, _ := os.UserHomeDir()

	venvDir := modelDir
	if venvDir == "" {
		venvDir = filepath.Join(home, ".aiko", "tts-venv")
	}
	// ~ 展开
	if len(venvDir) >= 2 && venvDir[:2] == "~/" {
		venvDir = filepath.Join(home, venvDir[2:])
	}

	pythonBin := filepath.Join(venvDir, "bin", "python3")
	modelsDir := filepath.Join(venvDir, "models")

	// 将内嵌脚本写入 ~/.aiko/kokoro_tts.py，每次启动都刷新保持最新。
	aikoDir := filepath.Join(home, ".aiko")
	_ = os.MkdirAll(aikoDir, 0755)
	scriptPath := filepath.Join(aikoDir, "kokoro_tts.py")
	_ = os.WriteFile(scriptPath, kokoroPyScript, 0644)

	return &KokoroSpeaker{
		pythonBin:  pythonBin,
		scriptPath: scriptPath,
		modelDir:   modelsDir,
	}, nil
}

// Speak 将文本合成为 WAV 字节，通过子进程调用 kokoro_tts.py。
func (k *KokoroSpeaker) Speak(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	if speed <= 0 {
		speed = 1.0
	}
	if voice == "" || voice == "default" {
		voice = kokoroVoices[0]
	}

	args := []string{
		k.scriptPath,
		"--voice", voice,
		"--speed", fmt.Sprintf("%.2f", speed),
		"--model-dir", k.modelDir,
		"--text", text,
	}

	cmd := exec.CommandContext(ctx, k.pythonBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kokoro: 子进程失败: %w\nstderr: %s", err, stderr.String())
	}
	wav := stdout.Bytes()
	if len(wav) < 44 {
		return nil, fmt.Errorf("kokoro: 输出不是有效 WAV（%d 字节）\nstderr: %s", len(wav), stderr.String())
	}
	return wav, nil
}

// Voices 返回 Kokoro 中文声线列表。
func (k *KokoroSpeaker) Voices(_ context.Context) ([]string, error) {
	return kokoroVoices, nil
}
