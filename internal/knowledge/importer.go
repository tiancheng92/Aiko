package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	lnpdf "github.com/ledongthuc/pdf"
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

// extractPDF extracts plain text from a PDF file using ledongthuc/pdf.
func extractPDF(path string) (string, error) {
	f, r, err := lnpdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	b, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("read pdf text: %w", err)
	}
	if _, err := io.Copy(&buf, b); err != nil {
		return "", fmt.Errorf("copy pdf text: %w", err)
	}
	return buf.String(), nil
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
		if !strings.HasSuffix(f.Name, ".html") && !strings.HasSuffix(f.Name, ".xhtml") {
			continue
		}
		// Wrap rc handling in a closure so defer runs per entry and panic from
		// the HTML parser doesn't leak the entry's reader.
		text, err := func(f *zip.File) (string, error) {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open epub entry %s: %w", f.Name, err)
			}
			defer rc.Close()
			return extractHTMLText(rc), nil
		}(f)
		if err != nil {
			return "", err
		}
		sb.WriteString(text)
		sb.WriteString("\n")
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
