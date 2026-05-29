package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

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
	MigratedToLong  bool     // already persisted to long-term vector memory
	CreatedAt       string
}

// ShortStore manages short-term conversation history in SQLite.
type ShortStore struct{ db *sql.DB }

// NewShortStore creates a ShortStore.
func NewShortStore(db *sql.DB) *ShortStore { return &ShortStore{db: db} }

// scanMessage scans a row that selects id, role, content, thinking_content, images, files, migrated_to_long, created_at.
func scanMessage(scan func(...any) error) (Message, error) {
	var m Message
	var imagesJSON, filesJSON string
	if err := scan(&m.ID, &m.Role, &m.Content, &m.ThinkingContent, &imagesJSON, &filesJSON, &m.MigratedToLong, &m.CreatedAt); err != nil {
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
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
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
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
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

// CountUnmigrated returns the number of messages not yet migrated to long-term memory.
func (s *ShortStore) CountUnmigrated() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE migrated_to_long = 0`).Scan(&n)
	return n, err
}

// escapeLikePattern escapes LIKE pattern wildcards so they are matched literally.
func escapeLikePattern(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return q
}

// Search returns all messages whose content matches the query via LIKE substring
// matching, newest first. Returns empty slice (not error) for empty query.
func (s *ShortStore) Search(query string) ([]Message, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	return s.searchLike(q)
}

// searchLike performs a SQL LIKE substring search.
func (s *ShortStore) searchLike(query string) ([]Message, error) {
	pattern := "%" + escapeLikePattern(query) + "%"
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
		FROM messages
		WHERE content LIKE ? ESCAPE '\'
		ORDER BY id DESC`, pattern)
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
	return msgs, rows.Err()
}

// GetNewestToID loads messages from the newest backwards, paginating by pageSize,
// until targetID is included. Returns all loaded messages in chronological order.
func (s *ShortStore) GetNewestToID(targetID int64, pageSize int) ([]Message, error) {
	var all []Message

	// First fetch: get most recent page.
	page, err := s.Recent(pageSize)
	if err != nil {
		return nil, fmt.Errorf("get newest to id: first page: %w", err)
	}
	if len(page) == 0 {
		return nil, nil
	}

	for {
		// Prepend page to all (page is chronological, prepend = older before newer).
		all = append(page, all...)

		// Check if target is in this page.
		found := false
		for _, m := range page {
			if m.ID == targetID {
				found = true
				break
			}
		}
		if found {
			break
		}

		// Fetch next older page.
		oldestID := page[0].ID
		page, err = s.BeforeID(oldestID, pageSize)
		if err != nil {
			return nil, fmt.Errorf("get newest to id: page before %d: %w", oldestID, err)
		}
		if len(page) == 0 {
			break // reached end, target not found
		}
	}

	return all, nil
}

// OldestN returns the oldest n messages in chronological order.
func (s *ShortStore) OldestN(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
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

// OldestUnmigratedN returns the oldest n messages that haven't been migrated to
// long-term memory, in chronological order.
func (s *ShortStore) OldestUnmigratedN(n int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
		FROM messages
		WHERE migrated_to_long = 0
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

// OldestUnmigratedAll returns all messages that haven't been migrated to
// long-term memory, in chronological order.
func (s *ShortStore) OldestUnmigratedAll() ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
		FROM messages
		WHERE migrated_to_long = 0
		ORDER BY id ASC`)
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

// RecentMessages returns the most recent n unmigrated messages as schema.Message
// objects, suitable for passing directly to runner.Run as multi-turn history.
// Messages already persisted to long-term memory are excluded — the LLM receives
// those via SearchSplit instead. Images and file attachments are omitted — the
// LLM has already processed them.
func (s *ShortStore) RecentMessages(n int) ([]*schema.Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
		FROM messages
		WHERE migrated_to_long = 0
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

	out := make([]*schema.Message, 0, len(msgs))
	for i := range msgs {
		if msgs[i].Content == "" {
			continue
		}
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

// MarkMigrated marks messages with the given IDs as migrated to long-term memory.
func (s *ShortStore) MarkMigrated(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	query := "UPDATE messages SET migrated_to_long = 1 WHERE id IN (" + placeholders + ")"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("mark migrated: %w", err)
	}
	return nil
}

// DeleteLastAssistantMessage removes the most recent assistant message from
// the store and returns it so the caller can re-use its preceding user message.
// Returns sql.ErrNoRows if no assistant message exists.
func (s *ShortStore) DeleteLastAssistantMessage() (Message, error) {
	var m Message
	err := s.db.QueryRow(`
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
		FROM messages
		WHERE role = 'assistant'
		ORDER BY id DESC
		LIMIT 1`).Scan(
		&m.ID, &m.Role, &m.Content, &m.ThinkingContent,
		new(string), new(string), &m.MigratedToLong, &m.CreatedAt,
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
		SELECT id, role, content, thinking_content, images, files, migrated_to_long, created_at
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

// formatBlockPool recycles strings.Builder instances used to assemble migration blocks.
var formatBlockPool = sync.Pool{New: func() any { return new(strings.Builder) }}

// FormatBlock formats a slice of messages into a single text block for storage.
func FormatBlock(msgs []Message) string {
	sb := formatBlockPool.Get().(*strings.Builder)
	sb.Reset()
	for i := range msgs {
		sb.WriteString(msgs[i].Role)
		sb.WriteString(": ")
		sb.WriteString(msgs[i].Content)
		sb.WriteString("\n")
	}
	s := sb.String()
	sb.Reset()
	formatBlockPool.Put(sb)
	return s
}
