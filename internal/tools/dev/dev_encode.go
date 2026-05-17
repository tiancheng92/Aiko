// internal/tools/dev/dev_encode.go
package dev

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/bytesconv"
	"aiko/internal/tools/base"
)

// ── 4. encode_decode ──────────────────────────────────────────────────────────

// EncodeDecodeTool encodes or decodes text in base64/URL/HTML formats.
type EncodeDecodeTool struct{}

func (t *EncodeDecodeTool) Name() string                    { return "encode_decode" }
func (t *EncodeDecodeTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for encode_decode.
func (t *EncodeDecodeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "Base64 / URL / HTML 编解码。",
		map[string]*schema.ParameterInfo{
			"text":   {Type: schema.String, Desc: "待处理的文本", Required: true},
			"format": {Type: schema.String, Desc: "base64 / base64url / url / html", Required: true},
			"action": {Type: schema.String, Desc: "encode 或 decode", Required: true},
		}), nil
}

// InvokableRun encodes or decodes the input text.
func (t *EncodeDecodeTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	text, _ := args["text"].(string)
	format, _ := args["format"].(string)
	action, _ := args["action"].(string)

	switch format {
	case "base64":
		if action == "encode" {
			return base64.StdEncoding.EncodeToString(bytesconv.StringToBytes(text)), nil
		}
		b, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return "", fmt.Errorf("base64 decode error: %w", err)
		}
		return string(b), nil

	case "base64url":
		if action == "encode" {
			return base64.URLEncoding.EncodeToString(bytesconv.StringToBytes(text)), nil
		}
		b, err := base64.URLEncoding.DecodeString(text)
		if err != nil {
			return "", fmt.Errorf("base64url decode error: %w", err)
		}
		return string(b), nil

	case "url":
		if action == "encode" {
			return url.QueryEscape(text), nil
		}
		decoded, err := url.QueryUnescape(text)
		if err != nil {
			return "", fmt.Errorf("url decode error: %w", err)
		}
		return decoded, nil

	case "html":
		if action == "encode" {
			return html.EscapeString(text), nil
		}
		return html.UnescapeString(text), nil

	default:
		return "", fmt.Errorf("unsupported format %q; use base64/base64url/url/html", format)
	}
}

// ── 5. hash_text ──────────────────────────────────────────────────────────────

// HashTextTool computes a cryptographic hash of the input text.
type HashTextTool struct{}

func (t *HashTextTool) Name() string                    { return "hash_text" }
func (t *HashTextTool) Permission() base.PermissionLevel { return base.PermPublic }

// Info returns the eino tool schema for hash_text.
func (t *HashTextTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return base.InfoFromSchema(t.Name(), "计算文本的 MD5 / SHA1 / SHA256 / SHA512 哈希值。",
		map[string]*schema.ParameterInfo{
			"text":      {Type: schema.String, Desc: "待哈希的文本", Required: true},
			"algorithm": {Type: schema.String, Desc: "md5 / sha1 / sha256 / sha512", Required: true},
			"encoding":  {Type: schema.String, Desc: "输出编码: hex（默认）/ base64"},
		}), nil
}

// InvokableRun hashes text using the specified algorithm.
func (t *HashTextTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := base.ParseArgs(input)
	text, _ := args["text"].(string)
	algorithm, _ := args["algorithm"].(string)
	encoding, _ := args["encoding"].(string)
	if encoding == "" {
		encoding = "hex"
	}

	var h []byte
	switch algorithm {
	case "md5":
		s := md5.Sum(bytesconv.StringToBytes(text))
		h = s[:]
	case "sha1":
		s := sha1.Sum(bytesconv.StringToBytes(text))
		h = s[:]
	case "sha256":
		s := sha256.Sum256(bytesconv.StringToBytes(text))
		h = s[:]
	case "sha512":
		s := sha512.Sum512(bytesconv.StringToBytes(text))
		h = s[:]
	default:
		return "", fmt.Errorf("unsupported algorithm %q; use md5/sha1/sha256/sha512", algorithm)
	}

	if encoding == "base64" {
		return base64.StdEncoding.EncodeToString(h), nil
	}
	return hex.EncodeToString(h), nil
}
