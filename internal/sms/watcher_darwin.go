//go:build darwin

// Package sms provides real-time SMS monitoring on macOS by watching
// the Messages SQLite database for new incoming SMS messages.
package sms

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"
	_ "modernc.org/sqlite"
)

// codePattern matches 4–8 digit verification codes.
var codePattern = regexp.MustCompile(`\b(\d{4,8})\b`)

// Event carries a detected verification code and its context.
type Event struct {
	Code   string `json:"code"`
	Sender string `json:"sender"`
	Text   string `json:"text"`
}

// Handler is called on each detected verification code event.
type Handler func(Event)

// Watcher monitors ~/Library/Messages/chat.db for new SMS messages
// and extracts verification codes via fsnotify on the parent directory.
type Watcher struct {
	handler   Handler
	dbDir     string // ~/Library/Messages/
	dbPath    string
	walPath   string
	lastRowID int64
	mu        sync.Mutex
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewWatcher creates a Watcher that calls handler for each detected code.
func NewWatcher(handler Handler) (*Watcher, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("sms watcher: get home dir: %w", err)
	}
	dbDir := filepath.Join(home, "Library", "Messages")
	dbPath := filepath.Join(dbDir, "chat.db")
	return &Watcher{
		handler: handler,
		dbDir:   dbDir,
		dbPath:  dbPath,
		walPath: dbPath + "-wal",
	}, nil
}

// Start begins watching for new SMS messages.
// It initialises lastRowID to the current max so only future messages are reported.
// Calling Start on a Watcher that is already running is a no-op.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		return nil // already running
	}
	w.mu.Unlock()

	db, err := openMessagesDB(w.dbPath)
	if err != nil {
		return fmt.Errorf("sms watcher: open db: %w", err)
	}

	// Seed lastRowID so we don't replay old messages on start.
	var maxID int64
	row := db.QueryRow(`SELECT COALESCE(MAX(ROWID),0) FROM message`)
	if scanErr := row.Scan(&maxID); scanErr != nil {
		db.Close()
		return fmt.Errorf("sms watcher: seed rowid: %w", scanErr)
	}
	db.Close()

	// Watch the parent directory instead of the WAL file directly.
	// This is resilient to WAL file deletion (checkpoint), creation, and rename.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("sms watcher: fsnotify: %w", err)
	}
	if err := watcher.Add(w.dbDir); err != nil {
		watcher.Close()
		return fmt.Errorf("sms watcher: watch dir %s: %w", w.dbDir, err)
	}

	watchCtx, cancel := context.WithCancel(ctx)

	w.mu.Lock()
	w.lastRowID = maxID
	w.cancel = cancel
	w.mu.Unlock()

	w.wg.Add(1)
	go w.loop(watchCtx, watcher)
	slog.Info("sms watcher started", "db", w.dbPath, "seed_rowid", maxID)
	return nil
}

// Stop gracefully shuts down the watcher.
func (w *Watcher) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	w.cancel = nil // clear so a subsequent Start() can restart cleanly
	w.mu.Unlock()

	if cancel != nil {
		cancel()
		w.wg.Wait()
	}
	slog.Info("sms watcher stopped")
}

// loop is the background goroutine driving the watcher.
func (w *Watcher) loop(ctx context.Context, fw *fsnotify.Watcher) {
	defer w.wg.Done()
	defer fw.Close()

	// Debounce: coalesce rapid consecutive writes (WAL checkpoints emit several).
	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	// Ensure the debounce timer is released regardless of which branch exits the
	// loop (ctx cancel, fw.Events close, fw.Errors close). Multiple Stop calls
	// are safe on time.Timer.
	defer debounce.Stop()
	pending := false

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-fw.Events:
			if !ok {
				return
			}
			// Trigger on any change to chat.db or chat.db-wal.
			name := filepath.Base(event.Name)
			if name != "chat.db" && name != "chat.db-wal" {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Chmod) {
				if !pending {
					debounce.Reset(300 * time.Millisecond)
					pending = true
				}
			}

		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			slog.Warn("sms watcher: fsnotify error", "err", err)

		case <-debounce.C:
			pending = false
			w.poll()
		}
	}
}

// poll queries the Messages DB for new SMS rows since lastRowID.
func (w *Watcher) poll() {
	w.mu.Lock()
	lastID := w.lastRowID
	w.mu.Unlock()

	db, err := openMessagesDB(w.dbPath)
	if err != nil {
		slog.Warn("sms watcher: poll open db", "err", err)
		return
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT m.ROWID, COALESCE(m.text,''), COALESCE(h.id,''), m.attributedBody
		FROM message m
		LEFT JOIN handle h ON m.handle_id = h.ROWID
		WHERE m.ROWID > ?
		  AND m.is_from_me = 0
		  AND m.service = 'SMS'
		ORDER BY m.ROWID ASC`, lastID)
	if err != nil {
		slog.Warn("sms watcher: poll query", "err", err)
		return
	}
	defer rows.Close()

	var newMax int64 = lastID
	for rows.Next() {
		var rowID int64
		var text, sender string
		var attributedBody []byte
		if err := rows.Scan(&rowID, &text, &sender, &attributedBody); err != nil {
			slog.Warn("sms watcher: scan row", "err", err)
			continue
		}
		// Always advance newMax even if no code is found, so we don't re-scan rows.
		if rowID > newMax {
			newMax = rowID
		}
		// message.text is empty on modern macOS; fall back to attributedBody.
		if text == "" && len(attributedBody) > 0 {
			text = extractTextFromAttributedBody(attributedBody)
		}
		if text == "" {
			continue
		}
		code := extractCode(text)
		slog.Info("sms watcher: new message", "rowid", rowID, "sender", sender, "text", text, "code", code)
		if code == "" {
			continue
		}
		slog.Info("sms watcher: verification code detected", "sender", sender, "code", code)
		w.handler(Event{Code: code, Sender: sender, Text: text})
	}

	if newMax > lastID {
		w.mu.Lock()
		w.lastRowID = newMax
		w.mu.Unlock()
	}
}

// extractCode returns the first numeric sequence that looks like a verification
// code (4–8 digits) from the message text, or "" if none is found.
// It only matches when the text contains a recognised OTP keyword; no fallback.
func extractCode(text string) string {
	lower := strings.ToLower(text)
	keywords := []string{"验证码", "动态密码", "校验码", "confirmation code", "verification code", "otp", "code is", "code:", "是您的"}
	for _, kw := range keywords {
		idx := strings.Index(lower, kw)
		if idx < 0 {
			continue
		}
		// Compute a safe byte window: 30 bytes before keyword, 40 bytes after.
		// Codes commonly appear BEFORE the keyword (e.g. "617387短信登录验证码").
		start := safeByteStart(text, idx, 30)
		end := safeByteEnd(text, idx+len(kw), 40)
		m := codePattern.FindString(text[start:end])
		if m != "" {
			return m
		}
	}
	return ""
}

// safeByteStart returns a byte offset that is at most n bytes before pos,
// adjusted backward to a valid UTF-8 rune boundary.
func safeByteStart(s string, pos, n int) int {
	start := pos - n
	if start <= 0 {
		return 0
	}
	// Walk forward to find the next valid UTF-8 start byte.
	for start < pos && !utf8.RuneStart(s[start]) {
		start++
	}
	return start
}

// safeByteEnd returns a byte offset that is at most n bytes after pos,
// clamped to len(s).
func safeByteEnd(s string, pos, n int) int {
	end := pos + n
	if end >= len(s) {
		return len(s)
	}
	// Walk backward to a valid UTF-8 start byte.
	for end > pos && !utf8.RuneStart(s[end]) {
		end--
	}
	return end
}

// openMessagesDB opens the Messages chat.db read-only.
func openMessagesDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=2000", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// extractTextFromAttributedBody extracts the plain-text string from a
// macOS typedstream-encoded NSMutableAttributedString blob (the attributedBody
// column in chat.db). On modern macOS, message.text is empty and the actual
// message content lives here.
//
// The typedstream format embeds UTF-8 strings as: 0x2B + length + bytes.
// When the high bit of the length byte is set (0x81, 0x82, …), it indicates
// a multi-byte length: the lower 7 bits tell how many bytes encode the actual
// length. We recursively skip such wrappers to reach the real string payload.
func extractTextFromAttributedBody(data []byte) string {
	best := ""
	i := 0
	for i < len(data)-2 {
		if data[i] != 0x2B {
			i++
			continue
		}
		text, consumed := readTypedstreamString(data, i)
		if consumed <= 0 {
			i++
			continue
		}
		if len(text) > len(best) {
			best = text
		}
		i += consumed
	}
	return best
}

// readTypedstreamString attempts to read a typedstream string at offset start.
// It returns (text, bytes_consumed) or ("", 0) on failure.
// Handles multi-byte length prefixes (0x81 nn, 0x82 nn nn, …).
func readTypedstreamString(data []byte, start int) (string, int) {
	if start >= len(data) || data[start] != 0x2B {
		return "", 0
	}
	pos := start + 1 // points at length byte(s)
	if pos >= len(data) {
		return "", 0
	}

	var length int
	lb := data[pos]
	if lb&0x80 == 0 {
		// Single-byte length.
		length = int(lb)
		pos++
	} else {
		// Multi-byte: lower 7 bits = number of following length bytes.
		numExtra := int(lb & 0x7F)
		pos++
		if pos+numExtra > len(data) {
			return "", 0
		}
		for k := 0; k < numExtra; k++ {
			length = (length << 8) | int(data[pos])
			pos++
		}
	}

	if length < 1 || pos+length > len(data) {
		return "", 0
	}

	// If the payload itself starts with another 0x2B, it's a nested wrapper —
	// recurse into it instead of treating the wrapper bytes as text.
	payload := data[pos : pos+length]
	if len(payload) >= 2 && payload[0] == 0x2B {
		inner, innerConsumed := readTypedstreamString(payload, 0)
		if innerConsumed > 0 && len(inner) > 0 {
			return inner, pos+length - start
		}
	}

	if !utf8.Valid(payload) {
		return "", 0
	}
	text := string(payload)
	// Must contain at least one printable non-pure-ASCII character or CJK/emoji
	// to qualify as message text (filter out ObjC class names like "NSString").
	hasContent := false
	for _, r := range text {
		if r > 0x7E || (r >= 'A' && r <= 'z') || r == ' ' || r == '：' {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return "", 0
	}
	return text, pos + length - start
}
