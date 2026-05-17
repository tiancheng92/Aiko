package memory

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	json "github.com/bytedance/sonic"
	"github.com/cloudwego/eino/schema"

	"aiko/internal/bytesconv"
)

// Message is a single conversation turn stored in SQLite.
type Message struct {
	ID              int64
	Role            string   // "user" | "assistant"
	Content         string
	ThinkingContent string   // LLM reasoning/thinking process, empty for most messages
	Images          []string // data URLs, empty for most messages
	Files           []string // attached file names (no content), empty for most messages
	CreatedAt       string
}

// ShortStore manages short-term conversation history in SQLite.
type ShortStore struct{ db *sql.DB }

// NewShortStore creates a ShortStore.
func NewShortStore(db *sql.DB) *ShortStore { return &ShortStore{db: db} }

// scanMessage scans a row that selects id, role, content, thinking_content, images, files, created_at.
func scanMessage(scan func(...any) error) (Message, error) {
	var m Message
	var imagesJSON, filesJSON string
	if err := scan(&m.ID, &m.Role, &m.Content, &m.ThinkingContent, &imagesJSON, &filesJSON, &m.CreatedAt); err != nil {
		return m, err
	}
	if imagesJSON != "" {
		if err := json.UnmarshalString(imagesJSON, &m.Images); err != nil {
			log.Warn().Int64("id", m.ID).Err(err).Msg("short memory: images JSON unmarshal")
		}
	}
	if filesJSON != "" {
		if err := json.UnmarshalString(filesJSON, &m.Files); err != nil {
			log.Warn().Int64("id", m.ID).Err(err).Msg("short memory: files JSON unmarshal")
		}
	}
	return m, nil
}

// Recent returns the most recent n messages in chronological order.
func (s *ShortStore) Recent(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		ORDER BY id DESC
		LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse to chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// Add inserts a new message (no images) and returns its ID.
func (s *ShortStore) Add(role, content string) (int64, error) {
	return s.AddWithImages(role, content, nil)
}

// AddWithImages inserts a new message with optional image data URLs and returns its ID.
func (s *ShortStore) AddWithImages(role, content string, images []string) (int64, error) {
	return s.AddWithImagesAndFiles(role, content, images, nil)
}

// AddFull inserts a new message with all optional fields and returns its ID.
func (s *ShortStore) AddFull(role, content, thinkingContent string, images []string, files []string) (int64, error) {
	imagesJSON := ""
	if len(images) > 0 {
		b, err := json.Marshal(images)
		if err != nil {
			log.Warn().Err(err).Msg("short memory: images JSON marshal")
		} else {
			imagesJSON = bytesconv.BytesToString(b)
		}
	}
	filesJSON := ""
	if len(files) > 0 {
		b, err := json.Marshal(files)
		if err != nil {
			log.Warn().Err(err).Msg("short memory: files JSON marshal")
		} else {
			filesJSON = bytesconv.BytesToString(b)
		}
	}
	res, err := s.db.Exec(
		`INSERT INTO messages(role, content, thinking_content, images, files) VALUES(?, ?, ?, ?, ?)`,
		role, content, thinkingContent, imagesJSON, filesJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AddWithImagesAndFiles inserts a new message with optional images and file names and returns its ID.
func (s *ShortStore) AddWithImagesAndFiles(role, content string, images []string, files []string) (int64, error) {
	return s.AddFull(role, content, "", images, files)
}

// BeforeID returns up to n messages with id < beforeID in chronological order.
func (s *ShortStore) BeforeID(beforeID int64, n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		WHERE id < ?
		ORDER BY id DESC
		LIMIT ?`, beforeID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// Count returns total number of stored messages.
func (s *ShortStore) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n)
	return n, err
}

// OldestN returns the oldest n messages in chronological order.
func (s *ShortStore) OldestN(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		ORDER BY id ASC
		LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

// DeleteAll removes all messages from the short-term store.
func (s *ShortStore) DeleteAll() error {
	_, err := s.db.Exec(`DELETE FROM messages`)
	return err
}

// RecentMessages returns the most recent n messages as schema.Message objects,
// suitable for passing directly to runner.Run as multi-turn history.
// Images and file attachments are omitted — the LLM has already processed them.
func (s *ShortStore) RecentMessages(n int) ([]*schema.Message, error) {
	msgs, err := s.Recent(n)
	if err != nil {
		return nil, err
	}
	out := make([]*schema.Message, 0, len(msgs))
	for i := range msgs {
		role := schema.User
		if msgs[i].Role == "assistant" {
			role = schema.Assistant
		}
		out = append(out, &schema.Message{Role: role, Content: msgs[i].Content})
	}
	return out, nil
}

// DeleteByIDs removes messages with the given IDs.
func (s *ShortStore) DeleteByIDs(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	query := "DELETE FROM messages WHERE id IN (" + placeholders + ")"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	return nil
}

// DeleteLastAssistantMessage removes the most recent assistant message from
// the store and returns it so the caller can re-use its preceding user message.
// Returns sql.ErrNoRows if no assistant message exists.
func (s *ShortStore) DeleteLastAssistantMessage() (Message, error) {
	var m Message
	err := s.db.QueryRow(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		WHERE role = 'assistant'
		ORDER BY id DESC
		LIMIT 1`).Scan(
		&m.ID, &m.Role, &m.Content, &m.ThinkingContent,
		new(string), new(string), &m.CreatedAt,
	)
	if err != nil {
		return m, err
	}
	_, err = s.db.Exec(`DELETE FROM messages WHERE id = ?`, m.ID)
	return m, err
}

// LastUserMessage returns the most recent user message.
// Returns sql.ErrNoRows if none exists.
func (s *ShortStore) LastUserMessage() (Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, created_at
		FROM messages
		WHERE role = 'user'
		ORDER BY id DESC
		LIMIT 1`)
	if err != nil {
		return Message{}, err
	}
	defer rows.Close()
	if rows.Next() {
		return scanMessage(rows.Scan)
	}
	return Message{}, sql.ErrNoRows
}

// FormatBlock formats a slice of messages into a single text block for storage.
func FormatBlock(msgs []Message) string {
	var sb strings.Builder
	for i := range msgs {
		sb.WriteString(msgs[i].Role)
		sb.WriteString(": ")
		sb.WriteString(msgs[i].Content)
		sb.WriteString("\n")
	}
	return sb.String()
}
