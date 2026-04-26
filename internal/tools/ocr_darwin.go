//go:build darwin

// internal/tools/ocr_darwin.go
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// ocrSwiftScript is the Vision OCR Swift program written once to a temp file.
const ocrSwiftScript = `import Vision
import AppKit
import Foundation

let path = CommandLine.arguments[1]
let imgURL = URL(fileURLWithPath: path)
guard let imgSrc = CGImageSourceCreateWithURL(imgURL as CFURL, nil),
      let cgImage = CGImageSourceCreateImageAtIndex(imgSrc, 0, nil) else {
    fputs("ERROR: could not load image at \(path)\n", stderr)
    exit(1)
}
let request = VNRecognizeTextRequest()
request.recognitionLevel = .accurate
request.usesLanguageCorrection = true
request.recognitionLanguages = ["zh-Hans", "zh-Hant", "en-US", "ja"]
let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
try? handler.perform([request])
let lines = (request.results as? [VNRecognizedTextObservation] ?? [])
    .compactMap { $0.topCandidates(1).first?.string }
print(lines.joined(separator: "\n"))
`

var (
	ocrScriptOnce sync.Once
	ocrScriptPath string
	ocrScriptErr  error
)

// ensureOCRScript writes the Swift OCR script to a stable temp path (once).
func ensureOCRScript() (string, error) {
	ocrScriptOnce.Do(func() {
		p := filepath.Join(os.TempDir(), "aiko_ocr_vision.swift")
		ocrScriptErr = os.WriteFile(p, []byte(ocrSwiftScript), 0o644)
		if ocrScriptErr == nil {
			ocrScriptPath = p
		}
	})
	return ocrScriptPath, ocrScriptErr
}

// InvokableRun captures the screen and performs OCR via macOS Vision.
func (t *OcrScreenTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	args := parseArgs(input)
	region, _ := args["region"].(string)

	// Build screencapture command.
	imgPath := filepath.Join(os.TempDir(), fmt.Sprintf("aiko_ocr_%d.png", time.Now().UnixNano()))
	defer os.Remove(imgPath)

	var captureArgs []string
	captureArgs = append(captureArgs, "-x") // silent
	if region != "" {
		// Validate format: "x,y,width,height"
		parts := strings.Split(region, ",")
		if len(parts) != 4 {
			return "OCR 失败：region 格式应为 \"x,y,width,height\"", nil
		}
		captureArgs = append(captureArgs, "-R", region)
	}
	captureArgs = append(captureArgs, imgPath)

	if out, err := exec.Command("screencapture", captureArgs...).CombinedOutput(); err != nil {
		return fmt.Sprintf("OCR 失败：截图错误 — %s\n请在「系统设置 → 隐私与安全性 → 屏幕录制」中授权 Aiko。", strings.TrimSpace(string(out))), nil
	}

	// Ensure the Swift script file exists.
	scriptPath, err := ensureOCRScript()
	if err != nil {
		return fmt.Sprintf("OCR 失败：无法写入 Swift 脚本 — %s", err.Error()), nil
	}

	// Run Swift OCR script.
	out, err := exec.Command("swift", scriptPath, imgPath).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Sprintf("OCR 失败：%s", result), nil
	}
	if result == "" {
		return "（未识别到文字）", nil
	}
	return result, nil
}
