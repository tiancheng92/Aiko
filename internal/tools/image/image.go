// internal/tools/image/image.go
package toolimage

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"

	"aiko/internal/tools/base"
)

// supportedImageExts maps file extensions to MIME types for read_image.
var supportedImageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// maxReadImageSize is the maximum image file size read_image will load (10 MB).
const maxReadImageSize = 10 * 1024 * 1024

// InvokableRun reads a local image file and returns it as a multimodal ToolResult.
func (t *ReadImageTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "read_image 不可用：配置未初始化", nil
	}
	args := base.ParseArgs(input)
	path, _ := args["path"].(string)
	if path == "" {
		return "参数 path 不能为空", nil
	}

	abs, err := base.CheckPath(path, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}

	ext := strings.ToLower(filepath.Ext(abs))
	mimeType, ok := supportedImageExts[ext]
	if !ok {
		return fmt.Sprintf("不支持的图片格式 %q，支持：png、jpg、jpeg、gif、webp", ext), nil
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("无法访问文件 %q: %v", abs, err), nil
	}
	if info.Size() > maxReadImageSize {
		return fmt.Sprintf("图片文件过大（%.1f MB），最大支持 10 MB", float64(info.Size())/1024/1024), nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("读取图片失败: %v", err), nil
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	// Return via plain string — the context accumulator in agent.go converts
	// data URLs in tool results into images for the conversation history.
	return "data:" + mimeType + ";base64," + b64, nil
}

// InvokableRun saves a base64 data URL or downloads an http(s) URL to a local file.
func (t *SaveImageTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	if t.Cfg == nil {
		return "save_image 不可用：配置未初始化", nil
	}
	args := base.ParseArgs(input)
	source, _ := args["source"].(string)
	destPath, _ := args["path"].(string)
	if source == "" {
		return "参数 source 不能为空", nil
	}
	if destPath == "" {
		return "参数 path 不能为空", nil
	}

	abs, err := base.CheckPath(destPath, t.Cfg.AllowedPaths)
	if err != nil {
		return err.Error(), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Sprintf("创建目录失败: %v", err), nil
	}

	var imageData []byte
	var detectedMIME string

	switch {
	case strings.HasPrefix(source, "data:"):
		// data:<mime>;base64,<data>
		rest := source[len("data:"):]
		semi := strings.Index(rest, ";base64,")
		if semi < 0 {
			return "无效的 data URL 格式，期望 data:<mime>;base64,<data>", nil
		}
		detectedMIME = rest[:semi]
		b64 := rest[semi+len(";base64,"):]
		imageData, err = base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return fmt.Sprintf("base64 解码失败: %v", err), nil
		}

	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return fmt.Sprintf("创建请求失败: %v", err), nil
		}
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			return fmt.Sprintf("下载图片失败: %v", err), nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Sprintf("下载失败，HTTP %d", resp.StatusCode), nil
		}
		detectedMIME = resp.Header.Get("Content-Type")
		imageData, err = io.ReadAll(io.LimitReader(resp.Body, maxReadImageSize+1))
		if err != nil {
			return fmt.Sprintf("读取图片数据失败: %v", err), nil
		}
		if int64(len(imageData)) > maxReadImageSize {
			return "图片文件过大（超过 10 MB），已取消保存", nil
		}

	default:
		return "source 必须是 http(s):// URL 或 data: URL", nil
	}

	// Infer extension from MIME type if dest path has no extension.
	if filepath.Ext(abs) == "" && detectedMIME != "" {
		exts, _ := mime.ExtensionsByType(detectedMIME)
		if len(exts) > 0 {
			abs += exts[0]
		}
	}

	if err := os.WriteFile(abs, imageData, 0o644); err != nil {
		return fmt.Sprintf("写入文件失败: %v", err), nil
	}
	return fmt.Sprintf("图片已保存到 %s（%d 字节）", abs, len(imageData)), nil
}
