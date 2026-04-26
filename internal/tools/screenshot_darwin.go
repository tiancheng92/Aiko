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

// InvokableRun captures a full-screen screenshot via screencapture and returns
// a ToolResult containing a ToolOutputImage so the model can see the screen.
func (t *TakeScreenshotTool) InvokableRun(ctx context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
	path := fmt.Sprintf("/tmp/aiko_shot_%d.png", time.Now().UnixNano())
	if err := exec.Command("screencapture", "-x", "-t", "png", path).Run(); err != nil {
		return nil, fmt.Errorf("screencapture: %w", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read screenshot: %w", err)
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
						MIMEType:   "image/png",
					},
				},
			},
		},
	}, nil
}
