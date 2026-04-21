package knowledge

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"golang.org/x/net/html"
)

const (
	chunkSize    = 512
	chunkOverlap = 64
)

// ImportProgress reports progress during import.
type ImportProgress struct {
	Source    string
	Total     int
	Processed int
}

// Import parses the file at path, splits into chunks, and stores them.
func Import(ctx context.Context, store *Store, path string, progress func(ImportProgress)) error {
	text, err := extractText(path)
	if err != nil {
		return fmt.Errorf("extract text from %s: %w", path, err)
	}
	chunks := splitChunks(text, chunkSize, chunkOverlap)
	source := filepath.Base(path)
	total := len(chunks)

	for i, chunk := range chunks {
		if err := store.AddChunk(ctx, chunk, source, i); err != nil {
			return fmt.Errorf("store chunk %d: %w", i, err)
		}
		if progress != nil {
			progress(ImportProgress{Source: source, Total: total, Processed: i + 1})
		}
	}
	return nil
}

// extractText reads a file and returns its plain text content.
func extractText(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md":
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case ".pdf":
		return extractPDF(path)
	case ".epub":
		return extractEPUB(path)
	default:
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
}

// extractPDF extracts plain text from a PDF file using pdfcpu.
// pdfcpu writes per-page .txt files to a temp directory; we read and concatenate them.
func extractPDF(path string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "desktop-pet-pdf-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := pdfapi.ExtractContentFile(path, tmpDir, nil, nil); err != nil {
		return "", fmt.Errorf("pdfcpu extract content: %w", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", fmt.Errorf("read temp dir: %w", err)
	}

	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tmpDir, e.Name()))
		if err != nil {
			return "", err
		}
		sb.Write(b)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// extractEPUB extracts plain text from an EPUB file (which is a ZIP of HTML files).
func extractEPUB(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open epub zip: %w", err)
	}
	defer r.Close()

	var sb strings.Builder
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, ".html") || strings.HasSuffix(f.Name, ".xhtml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			text := extractHTMLText(rc)
			rc.Close()
			sb.WriteString(text)
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

// extractHTMLText walks an HTML parse tree and collects all text nodes.
func extractHTMLText(r io.Reader) string {
	doc, err := html.Parse(r)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				sb.WriteString(t)
				sb.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return sb.String()
}

// splitChunks splits text into overlapping chunks of rune-length size with the given overlap.
func splitChunks(text string, size, overlap int) []string {
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "")
	}
	runes := []rune(text)
	var chunks []string
	step := size - overlap
	if step <= 0 {
		step = size
	}
	for start := 0; start < len(runes); start += step {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
