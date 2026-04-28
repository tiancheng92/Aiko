# File Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users attach readable text files to chat messages; file content is sent to the LLM but only filenames are persisted in memory/DB; front-end shows file name + icon chips.

**Architecture:** A new `FileAttachment` struct flows from the frontend through `app.go`'s `SendMessageWithFiles`, which appends file content as formatted text to the LLM message while passing only filenames via `msg.Extra` to `persistAndMigrate`. The DB gains a `files` TEXT column (idempotent migration), and `memory.Message` gains a `Files []string` field to carry filenames back to the frontend on history load.

**Tech Stack:** Go (eino `schema.MessageInputPart` text parts, Wails bindings), Vue 3 `<script setup>`, existing `FileReader` API.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/db/sqlite.go` | Modify | Add `files` column migration patch |
| `internal/memory/short.go` | Modify | Add `Files` field, `AddWithImagesAndFiles`, update scanners |
| `internal/agent/agent.go` | Modify | `persistAndMigrate` accepts `userFiles`, `ChatWithMessage` extracts from Extra |
| `app.go` | Modify | Add `FileAttachment` type + `SendMessageWithFiles` method |
| `frontend/src/components/ChatPanel.vue` | Modify | File picker, validation, pending chips, message bubble chips, send() wiring |

---

## Task 1: DB Migration — add `files` column

**Files:**
- Modify: `internal/db/sqlite.go:119-131`

- [ ] **Step 1: Add the migration patch**

In `sqlite.go`, find the `patches` slice (around line 119) and add a second entry:

```go
patches := []string{
    // v2: store images as JSON array of data URLs alongside each message.
    `ALTER TABLE messages ADD COLUMN images TEXT NOT NULL DEFAULT ''`,
    // v3: store attached file names as JSON array alongside each message.
    `ALTER TABLE messages ADD COLUMN files TEXT NOT NULL DEFAULT ''`,
}
```

- [ ] **Step 2: Verify the DB compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./internal/db/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/db/sqlite.go
git commit -m "feat(db): add files column to messages table (migration v3)"
```

---

## Task 2: memory.Message — add Files field and AddWithImagesAndFiles

**Files:**
- Modify: `internal/memory/short.go`

- [ ] **Step 1: Add `Files` field to `Message` struct**

Replace the struct definition:

```go
// Message is a single conversation turn stored in SQLite.
type Message struct {
	ID        int64
	Role      string   // "user" | "assistant"
	Content   string
	Images    []string // data URLs, empty for most messages
	Files     []string // attached file names (no content), empty for most messages
	CreatedAt string
}
```

- [ ] **Step 2: Update `scanMessage` to scan the `files` column**

Replace `scanMessage`:

```go
// scanMessage scans a row that selects id, role, content, images, files, created_at.
func scanMessage(scan func(...any) error) (Message, error) {
	var m Message
	var imagesJSON, filesJSON string
	if err := scan(&m.ID, &m.Role, &m.Content, &imagesJSON, &filesJSON, &m.CreatedAt); err != nil {
		return m, err
	}
	if imagesJSON != "" {
		_ = json.Unmarshal([]byte(imagesJSON), &m.Images)
	}
	if filesJSON != "" {
		_ = json.Unmarshal([]byte(filesJSON), &m.Files)
	}
	return m, nil
}
```

- [ ] **Step 3: Update all SELECT queries to include `files`**

In `Recent` (around line 40), update the query:

```go
rows, err := s.db.Query(`
    SELECT id, role, content, images, files, created_at
    FROM messages
    ORDER BY id DESC
    LIMIT ?`, n)
```

In `OldestN` (around line 97), update the query:

```go
rows, err := s.db.Query(`
    SELECT id, role, content, images, files, created_at
    FROM messages
    ORDER BY id ASC
    LIMIT ?`, n)
```

- [ ] **Step 4: Add `AddWithImagesAndFiles` and update `AddWithImages`**

Replace `AddWithImages` and add the new method right below it:

```go
// AddWithImages inserts a new message with optional image data URLs and returns its ID.
func (s *ShortStore) AddWithImages(role, content string, images []string) (int64, error) {
	return s.AddWithImagesAndFiles(role, content, images, nil)
}

// AddWithImagesAndFiles inserts a new message with optional images and file names and returns its ID.
func (s *ShortStore) AddWithImagesAndFiles(role, content string, images []string, files []string) (int64, error) {
	imagesJSON := ""
	if len(images) > 0 {
		b, _ := json.Marshal(images)
		imagesJSON = string(b)
	}
	filesJSON := ""
	if len(files) > 0 {
		b, _ := json.Marshal(files)
		filesJSON = string(b)
	}
	res, err := s.db.Exec(
		`INSERT INTO messages(role, content, images, files) VALUES(?, ?, ?, ?)`,
		role, content, imagesJSON, filesJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
```

- [ ] **Step 5: Verify compilation**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./internal/memory/...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/memory/short.go
git commit -m "feat(memory): add Files field and AddWithImagesAndFiles to short-term store"
```

---

## Task 3: agent.go — thread file names through ChatWithMessage and persistAndMigrate

**Files:**
- Modify: `internal/agent/agent.go:433-478` (ChatWithMessage)
- Modify: `internal/agent/agent.go:557-574` (persistAndMigrate)

- [ ] **Step 1: Update `persistAndMigrate` signature to accept file names**

Find `persistAndMigrate` (around line 557) and change its signature and the `AddWithImages` call:

```go
// persistAndMigrate saves user and assistant messages to SQLite, then checks
// whether the total message count exceeds ShortTermLimit. If so, the oldest
// excess messages are migrated to long-term vector memory.
func (a *Agent) persistAndMigrate(ctx context.Context, userInput string, userImages []string, userFiles []string, assistantReply string) {
	if a.shortMem == nil {
		return
	}

	a.turnCount.Add(1)

	if _, err := a.shortMem.AddWithImagesAndFiles("user", userInput, userImages, userFiles); err != nil {
		slog.Error("save user message failed", "err", err)
		return
	}
	// ... rest of the function unchanged
```

- [ ] **Step 2: Update `ChatWithMessage` to extract file names from `msg.Extra` and pass to `persistAndMigrate`**

In `ChatWithMessage` (around line 474-477), replace the persist call block:

```go
ch <- StreamResult{Done: true}
// Prefer the original user text stored in Extra (no file content) for memory.
userMemory := extractTextFromMessage(msg)
if orig, ok := msg.Extra["_user_text"].(string); ok && orig != "" {
    userMemory = orig
}
userImages := extractImagesFromMessage(msg)
// Extract file names passed via Extra by app.go.
var userFiles []string
if raw, ok := msg.Extra["_file_names"]; ok {
    if names, ok := raw.([]string); ok {
        userFiles = names
    }
}
go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse)
```

- [ ] **Step 3: Verify compilation**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./internal/agent/...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): thread file names through ChatWithMessage and persistAndMigrate"
```

---

## Task 4: app.go — FileAttachment type + SendMessageWithFiles

**Files:**
- Modify: `app.go` (add after `SendMessageWithImages`, around line 868)

- [ ] **Step 1: Add `FileAttachment` struct**

After the `App` struct definition (or near the top of `app.go` after imports), add:

```go
// FileAttachment carries a single text-file attachment from the frontend.
// Content is the full UTF-8 text of the file; Name and MimeType are metadata only.
type FileAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Content  string `json:"content"`
}
```

- [ ] **Step 2: Add `SendMessageWithFiles` method**

Add after `SendMessageWithImages` (after line ~868):

```go
// SendMessageWithFiles streams an AI response for a user message that may include
// inline images (data URLs) and/or text file attachments.
// File contents are appended to the user text before sending to the LLM.
// Only file names are persisted in memory — not the content.
func (a *App) SendMessageWithFiles(userInput string, images []string, files []FileAttachment) error {
	// Cancel any previous in-flight request.
	a.mu.Lock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
	chatCtx, cancel := context.WithCancel(a.ctx)
	a.chatCancel = cancel
	a.chatGeneration++
	myGen := a.chatGeneration
	a.mu.Unlock()

	a.mu.RLock()
	ag := a.petAgent
	a.mu.RUnlock()

	if ag == nil {
		a.mu.Lock()
		a.chatCancel = nil
		a.mu.Unlock()
		cancel()
		return fmt.Errorf("agent not initialized: complete settings first")
	}

	// Build LLM text: original input + file contents appended.
	llmText := userInput
	fileNames := make([]string, 0, len(files))
	for _, f := range files {
		fileNames = append(fileNames, f.Name)
		llmText += fmt.Sprintf("\n\n[文件: %s (%s)]\n```\n%s\n```", f.Name, f.MimeType, f.Content)
	}

	// Build UserInputMultiContent: text part first, then image parts.
	parts := make([]schema.MessageInputPart, 0, 1+len(images))
	parts = append(parts, schema.MessageInputPart{
		Type: schema.ChatMessagePartTypeText,
		Text: llmText,
	})
	for _, dataURL := range images {
		mimeType, b64data, ok := parseDataURL(dataURL)
		if !ok {
			slog.Warn("SendMessageWithFiles: invalid data URL, skipping")
			continue
		}
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{
					Base64Data: &b64data,
					MIMEType:   mimeType,
				},
			},
		})
	}

	msg := &schema.Message{
		Role:                  schema.User,
		UserInputMultiContent: parts,
		Extra: map[string]any{
			"_user_text":   userInput,
			"_file_names":  fileNames,
		},
	}

	go func() {
		defer cancel()
		defer func() {
			a.mu.Lock()
			if a.chatGeneration == myGen {
				a.chatCancel = nil
			}
			a.mu.Unlock()
		}()
		ch := ag.ChatWithMessage(chatCtx, msg)
		for result := range ch {
			if result.Err != nil {
				if errors.Is(result.Err, context.Canceled) {
					return
				}
				wailsruntime.EventsEmit(a.ctx, "chat:error", result.Err.Error())
				return
			}
			if result.Done {
				wailsruntime.EventsEmit(a.ctx, "chat:done", "")
				return
			}
			wailsruntime.EventsEmit(a.ctx, "chat:token", result.Token)
		}
		wailsruntime.EventsEmit(a.ctx, "chat:done", "")
	}()
	return nil
}
```

- [ ] **Step 3: Verify compilation**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no output.

- [ ] **Step 4: Regenerate Wails bindings**

```bash
cd /Users/xutiancheng/code/self/Aiko && wails generate module
```

Expected: `frontend/src/wailsjs/go/main/App.js` and `App.d.ts` updated with `SendMessageWithFiles`.

- [ ] **Step 5: Commit**

```bash
git add app.go frontend/src/wailsjs/
git commit -m "feat(app): add FileAttachment type and SendMessageWithFiles binding"
```

---

## Task 5: Frontend — file picker, validation, pending chips, message chips, send() wiring

**Files:**
- Modify: `frontend/src/components/ChatPanel.vue`

### 5a: Script — imports, refs, validation, send() wiring

- [ ] **Step 1: Add `SendMessageWithFiles` to imports**

In the import line at the top of `<script setup>` (line 3), add `SendMessageWithFiles`:

```js
import { SendMessage, SendMessageWithImages, SendMessageWithFiles, GetMessages, ClearChatHistory, IsFirstLaunch, MarkWelcomeShown, GetVoiceAutoSend, StopGeneration, SpeakText, StopTTS, GetConfig } from '../../wailsjs/go/main/App'
```

- [ ] **Step 2: Add `pendingFiles` ref and `fileInputEl` ref after `pendingImages`**

After line `const pendingImages = ref([])` (around line 94), add:

```js
/** pendingFiles holds text files selected by the user, awaiting send. */
const pendingFiles = ref([])
/** fileInputEl is the hidden <input type="file"> element for triggering the OS picker. */
const fileInputEl = ref(null)
```

- [ ] **Step 3: Add MIME whitelist and `addFile` validation function**

After `removeImage` function (around line 384), add:

```js
const READABLE_MIME_PREFIXES = ['text/']
const READABLE_MIME_EXACT = new Set([
  'application/json',
  'application/xml',
  'application/javascript',
  'application/typescript',
  'application/x-sh',
  'application/x-python',
])
const MAX_FILE_BYTES = 200 * 1024

/** isReadableMime returns true if the MIME type is a supported text type. */
function isReadableMime(mime) {
  if (READABLE_MIME_PREFIXES.some(p => mime.startsWith(p))) return true
  return READABLE_MIME_EXACT.has(mime)
}

/** addFile validates and reads a File object, pushing to pendingFiles on success. */
function addFile(file) {
  if (file.size > MAX_FILE_BYTES) {
    messages.value.push({ role: 'system', content: `文件过大（最大 200KB）：${file.name}` })
    return
  }
  const mime = file.type || 'text/plain'
  if (!isReadableMime(mime)) {
    messages.value.push({ role: 'system', content: `不支持此文件类型，仅支持文本文件：${file.name}` })
    return
  }
  const reader = new FileReader()
  reader.onload = (ev) => {
    pendingFiles.value.push({ name: file.name, mimeType: mime, content: ev.target.result })
  }
  reader.readAsText(file)
}

/** onFileInputChange handles files selected via the OS file picker. */
function onFileInputChange(e) {
  for (const file of e.target.files) {
    addFile(file)
  }
  e.target.value = ''
}

/** removeFile removes a pending file by index. */
function removeFile(idx) {
  pendingFiles.value.splice(idx, 1)
}
```

- [ ] **Step 4: Update `send()` to handle files**

Replace the `send` function (lines 387-416):

```js
/** send submits the current input as a user message. */
async function send() {
  const text = input.value.trim()
  if ((!text && pendingImages.value.length === 0 && pendingFiles.value.length === 0) || loading.value) return
  input.value = ''
  loading.value = true
  isStreaming.value = true
  firstTokenThisTurn = true
  if (soundsEnabled) playSend()

  const imgs = [...pendingImages.value]
  pendingImages.value = []
  const fileAttachments = pendingFiles.value.map(f => ({ name: f.name, mimeType: f.mimeType, content: f.content }))
  const fileNames = pendingFiles.value.map(f => f.name)
  pendingFiles.value = []

  messages.value.push({ role: 'user', content: text, images: imgs, files: fileNames, time: new Date() })
  messages.value.push({ role: 'assistant', content: '', streaming: true, thinking: true })
  scrollToBottom()
  EventsEmit('pet:state:change', 'thinking')
  try {
    if (imgs.length > 0 || fileAttachments.length > 0) {
      await SendMessageWithFiles(text, imgs, fileAttachments)
    } else {
      await SendMessage(text)
    }
  } catch (e) {
    const idx = messages.value.findLastIndex(m => m.thinking)
    if (idx >= 0) messages.value.splice(idx, 1)
    messages.value.push({ role: 'system', content: '发送失败: ' + e })
    loading.value = false
    isStreaming.value = false
    EventsEmit('pet:state:change', 'error')
  }
}
```

- [ ] **Step 5: Update history load to include `files`**

In `onMounted` (around line 154), update the history map:

```js
messages.value = (history || []).map(m => ({
  role: m.Role,
  content: m.Content,
  time: m.CreatedAt,
  images: m.Images || [],
  files: m.Files || [],
}))
```

### 5b: Template — file picker button, pending file chips, message file chips

- [ ] **Step 6: Add hidden file input and attach button to input row**

Find the `<div class="input-row">` block (around line 549). Add a hidden file input and an attach button before the textarea:

```html
<div class="input-row">
  <input
    ref="fileInputEl"
    type="file"
    multiple
    style="display:none"
    @change="onFileInputChange"
  />
  <button
    class="attach-btn"
    title="附加文件"
    :disabled="loading"
    @click="fileInputEl.click()"
  >
    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>
  </button>
  <textarea
    ref="textareaEl"
    v-model="input"
    placeholder="输入消息... (Enter 发送)"
    rows="1"
    @keydown.enter.exact.prevent="send"
    @paste="onPaste"
    :disabled="loading"
  />
  <button v-if="isStreaming" class="stop-btn" @click="stopGeneration">⏹ 停止</button>
  <button v-else @click="send" :disabled="loading">发送</button>
</div>
```

- [ ] **Step 7: Add pending file chips above the input row**

Find the pending images block (around line 542). Add file chips right after it (before `<div class="input-row">`):

```html
<!-- Pending file chips shown above the input row -->
<div v-if="pendingFiles.length > 0" class="pending-files">
  <div v-for="(f, idx) in pendingFiles" :key="idx" class="pending-file-chip">
    <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
    <span class="pending-file-name">{{ f.name }}</span>
    <button class="pending-file-remove" @click="removeFile(idx)">×</button>
  </div>
</div>
```

- [ ] **Step 8: Add file chips inside sent user message bubbles**

Find the message bubble template block for non-assistant messages (around line 476). After the `<div v-if="m.images ...">` block, add:

```html
<div v-if="m.files && m.files.length > 0" class="msg-files">
  <div v-for="(fname, fi) in m.files" :key="fi" class="msg-file-chip">
    <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
    <span>{{ fname }}</span>
  </div>
</div>
```

Also update the `has-images` class condition to cover files too:

```html
<div v-if="m.role !== 'assistant'" class="bubble markdown" :class="{ 'has-images': (m.images && m.images.length > 0) || (m.files && m.files.length > 0) }">
```

### 5c: Styles — chips and attach button

- [ ] **Step 9: Add CSS for new elements**

Append to the `<style scoped>` section (before the closing `</style>`):

```css
/* Attach file button */
.attach-btn {
  flex-shrink: 0;
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(156,163,175,0.8);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  padding: 0;
}
.attach-btn:hover:not(:disabled) { background: rgba(255,255,255,0.1); color: #f9fafb; }
.attach-btn:disabled { opacity: 0.35; cursor: not-allowed; }

/* Pending file chips above input */
.pending-files {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 6px 12px 0;
}
.pending-file-chip {
  display: flex;
  align-items: center;
  gap: 5px;
  background: rgba(99,102,241,0.15);
  border: 1px solid rgba(99,102,241,0.3);
  border-radius: 8px;
  padding: 4px 8px;
  font-size: 12px;
  color: #a5b4fc;
  max-width: 220px;
}
.pending-file-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.pending-file-remove {
  background: none;
  border: none;
  color: rgba(165,180,252,0.7);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 0;
  flex-shrink: 0;
  box-shadow: none;
}
.pending-file-remove:hover { color: #f9fafb; }

/* File chips inside sent user messages */
.msg-files {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-bottom: 4px;
}
.msg-file-chip {
  display: flex;
  align-items: center;
  gap: 4px;
  background: rgba(255,255,255,0.12);
  border: 1px solid rgba(255,255,255,0.18);
  border-radius: 6px;
  padding: 3px 8px;
  font-size: 11.5px;
  color: rgba(255,255,255,0.85);
  max-width: 200px;
  overflow: hidden;
  white-space: nowrap;
}
.msg-file-chip span {
  overflow: hidden;
  text-overflow: ellipsis;
}
```

- [ ] **Step 10: Build the frontend to check for errors**

```bash
cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build 2>&1 | tail -20
```

Expected: `✓ built in ...` with no errors.

- [ ] **Step 11: Commit**

```bash
cd /Users/xutiancheng/code/self/Aiko
git add frontend/src/components/ChatPanel.vue
git commit -m "feat(ui): file upload — picker, validation, pending chips, message chips"
```

---

## Task 6: End-to-end smoke test

- [ ] **Step 1: Build and run the app**

```bash
cd /Users/xutiancheng/code/self/Aiko && make run
```

- [ ] **Step 2: Test happy path — small text file**

  1. Click the 📎 button in the chat input row.
  2. Select any `.txt` or `.go` file under 200KB.
  3. Verify a file chip appears above the input with the correct filename.
  4. Type a message like "summarize this file" and press Enter.
  5. Verify the sent message bubble shows the file chip + your text.
  6. Verify the LLM response references the file content.

- [ ] **Step 3: Test oversized file rejection**

  1. Click 📎 and select a file > 200KB.
  2. Verify a red system bubble appears: "文件过大（最大 200KB）：filename".
  3. Verify no chip is added to pending.

- [ ] **Step 4: Test binary/image file rejection**

  1. Click 📎 and select a `.png` or `.exe` file.
  2. Verify a red system bubble appears: "不支持此文件类型，仅支持文本文件：filename".
  3. Verify no chip is added to pending.

- [ ] **Step 5: Test combined image + file**

  1. Paste an image (existing flow) and also attach a `.json` file.
  2. Send. Verify both appear in the message bubble and the LLM receives both.

- [ ] **Step 6: Test history persistence**

  1. Quit and relaunch the app (or clear and reload).
  2. Verify previously sent messages still show file chips with correct names.
  3. Verify file content is NOT shown in the message text.

- [ ] **Step 7: Final commit if all tests pass**

```bash
cd /Users/xutiancheng/code/self/Aiko
git add -p  # review any lingering uncommitted changes
git commit -m "feat: file upload end-to-end verified"
```
