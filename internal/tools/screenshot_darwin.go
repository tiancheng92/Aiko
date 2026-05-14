//go:build darwin

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// maxScreenshotWidth is the maximum pixel width passed to sips when resizing.
// Keeping screenshots narrow dramatically reduces base64 size and LLM token cost.
const maxScreenshotWidth = 1280

// InvokableRun captures a full-screen screenshot via screencapture, resizes it
// to at most maxScreenshotWidth pixels wide and re-encodes as JPEG (quality 75)
// using macOS's built-in sips tool, then returns the compressed image so the
// model can see the screen without burning excessive context tokens.
func (t *TakeScreenshotTool) InvokableRun(ctx context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
	ts := time.Now().UnixNano()
	pngPath := fmt.Sprintf("/tmp/aiko_shot_%d.png", ts)
	jpgPath := fmt.Sprintf("/tmp/aiko_shot_%d.jpg", ts)

	if err := exec.Command("screencapture", "-x", "-t", "png", pngPath).Run(); err != nil {
		return nil, fmt.Errorf("screencapture: %w", err)
	}
	defer os.Remove(pngPath)
	defer os.Remove(jpgPath)

	// Resize to max width and convert to JPEG at quality 75 using sips (built-in on macOS).
	// This typically shrinks a 1-2 MB Retina PNG down to ~80-150 KB.
	if err := exec.Command("sips",
		"--resampleWidth", fmt.Sprintf("%d", maxScreenshotWidth),
		"-s", "format", "jpeg",
		"-s", "formatOptions", "75",
		pngPath, "--out", jpgPath,
	).Run(); err != nil {
		// Fall back to the original PNG if sips fails.
		jpgPath = pngPath
	}

	data, err := os.ReadFile(jpgPath)
	if err != nil {
		return nil, fmt.Errorf("read screenshot: %w", err)
	}

	mime := "image/jpeg"
	if jpgPath == pngPath {
		mime = "image/png"
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{
				Type: schema.ToolPartTypeText,
				Text: "截图已完成，图片内容如下：",
			},
			{
				Type: schema.ToolPartTypeImage,
				Image: &schema.ToolOutputImage{
					MessagePartCommon: schema.MessagePartCommon{
						Base64Data: &b64,
						MIMEType:   mime,
					},
				},
			},
		},
	}, nil
}
