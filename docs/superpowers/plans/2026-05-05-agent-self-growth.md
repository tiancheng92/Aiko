# Agent Self-Growth Efficiency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move self-growth nudge from synchronous injection to async post-reply reflection, add event-driven triggers, enforce search-before-save, use a structured fill-in-the-blank prompt, and feed skill activation back into the reflection loop.

**Architecture:** All changes confined to `internal/agent/agent.go`. Two new methods (`shouldReflect`, `reflect`) and one new function (`buildReflectionPrompt`) replace the current `nudgeText` constant and its injection sites in `Chat()` / `ChatWithMessage()`. A new `lastSkillHint` field (protected by `skillHintMu sync.Mutex`) carries skill activation signals from middleware into `persistAndMigrate`.

**Tech Stack:** Go standard library (`strings`, `sync`, `log/slog`), existing `adk.Runner`, existing `ChatDirectCollect`.

---

## File Map

| File | Change |
|------|--------|
| `internal/agent/agent.go` | All changes — remove nudge injection, add struct fields, add methods, update `persistAndMigrate` |

No other files change.

---

### Task 1: Remove sync nudge, add struct fields

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Delete `nudgeText` constant and remove nudge injection from `Chat()`**

In `agent.go`, delete lines 32–39 (the `nudgeText` const block) and delete lines 226–229 in `Chat()`:

```go
// DELETE these lines from Chat():
content := userInput
if a.nudgeInterval > 0 && a.turnCount.Load() > 0 &&
    a.turnCount.Load()%int64(a.nudgeInterval) == 0 {
    content += "\n\n" + nudgeText
}

// REPLACE with:
content := userInput
```

Then update the `msgs` build line to use `content` (it already does — no change needed there).

- [ ] **Step 2: Remove nudge injection from `ChatWithMessage()`**

Delete lines 493–509 in `ChatWithMessage()` (the `sendMsg` / nudge block):

```go
// DELETE this block:
sendMsg := msg
if a.nudgeInterval > 0 && a.turnCount.Load() > 0 &&
    a.turnCount.Load()%int64(a.nudgeInterval) == 0 {
    cp := *msg
    if cp.Content != "" {
        cp.Content += "\n\n" + nudgeText
    } else {
        parts := make([]schema.MessageInputPart, len(msg.UserInputMultiContent)+1)
        copy(parts, msg.UserInputMultiContent)
        parts[len(parts)-1] = schema.MessageInputPart{
            Type: schema.ChatMessagePartTypeText,
            Text: nudgeText,
        }
        cp.UserInputMultiContent = parts
    }
    sendMsg = &cp
}

// REPLACE with:
sendMsg := msg
```

- [ ] **Step 3: Add new struct fields to `Agent`**

In the `Agent` struct definition (currently ending at `emitEvent`), add:

```go
type Agent struct {
    runner          *adk.Runner
    shortMem        *memory.ShortStore
    longMem         *memory.LongStore
    cfg             *config.Config
    dataDir         string
    turnCount       atomic.Int64
    nudgeInterval   int
    pendingConfirms *sync.Map
    emitEvent       func(event string, data ...any)
    // self-growth
    hasSkills     bool        // true if auto-skills/ directory has any *.md files at startup
    lastSkillHint string      // set by SetSkillHint; cleared by persistAndMigrate
    skillHintMu   sync.Mutex  // guards lastSkillHint
}
```

- [ ] **Step 4: Initialise `hasSkills` in `New()`**

At the end of `New()`, just before the `return` statement, add:

```go
skillsDir := filepath.Join(dataDir, "auto-skills")
entries, _ := os.ReadDir(skillsDir)
hasSkills := false
for _, e := range entries {
    if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
        hasSkills = true
        break
    }
}

return &Agent{
    runner:          runner,
    shortMem:        shortMem,
    longMem:         longMem,
    cfg:             cfg,
    dataDir:         dataDir,
    nudgeInterval:   ni,
    pendingConfirms: pendingConfirms,
    emitEvent:       emitEvent,
    hasSkills:       hasSkills,
}, nil
```

- [ ] **Step 5: Add `SetSkillHint` method (called by skill middleware)**

Add this exported method after `New()`:

```go
// SetSkillHint records that a skill was activated during this turn.
// Called by the skill middleware after injecting a skill into the prompt.
// The hint is consumed and cleared by persistAndMigrate after the reply.
func (a *Agent) SetSkillHint(skillName string) {
    a.skillHintMu.Lock()
    defer a.skillHintMu.Unlock()
    a.lastSkillHint = fmt.Sprintf("本次激活了 skill %q，回复结束后思考该 skill 是否需要更新。", skillName)
}
```

- [ ] **Step 6: Verify it compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/agent.go
git commit -m "refactor: remove sync nudge injection, add self-growth struct fields"
```

---

### Task 2: Add `shouldReflect` and `buildReflectionPrompt`

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add `shouldReflect` method**

Add after `SetSkillHint`:

```go
// shouldReflect decides whether to trigger a self-growth reflection after this turn.
// Returns (trigger, hints). trigger is true under any of:
//   - periodic: turnCount % nudgeInterval == 0
//   - user correction keywords detected in userInput
//   - multi-tool chain (≥3 tool-call blocks) detected in assistantReply
//   - explicit preference keywords detected in userInput
//   - hints is non-empty (skill activation signal from middleware)
func (a *Agent) shouldReflect(userInput, assistantReply string) (bool, []string) {
    var hints []string

    // Collect skill-activation hint (cleared here, not in persistAndMigrate).
    a.skillHintMu.Lock()
    if a.lastSkillHint != "" {
        hints = append(hints, a.lastSkillHint)
        a.lastSkillHint = ""
    }
    a.skillHintMu.Unlock()

    // Periodic trigger.
    tc := a.turnCount.Load()
    if a.nudgeInterval > 0 && tc > 0 && tc%int64(a.nudgeInterval) == 0 {
        return true, hints
    }

    // User correction signal.
    correctionKW := []string{"不对", "你刚才说错了", "其实是", "纠正"}
    for _, kw := range correctionKW {
        if strings.Contains(userInput, kw) {
            hints = append(hints, "检测到用户纠错，检查相关 skill 是否需要更新。")
            break
        }
    }

    // Multi-tool chain signal: count "\"tool_calls\"" occurrences in assistantReply.
    toolCallCount := strings.Count(assistantReply, `"tool_calls"`)
    if toolCallCount >= 3 {
        hints = append(hints, fmt.Sprintf("本次涉及 %d 个工具调用，考虑提炼为可复用技能。", toolCallCount))
    }

    // Explicit preference signal.
    prefKW := []string{"以后都", "我不喜欢", "记住", "每次都"}
    for _, kw := range prefKW {
        if strings.Contains(userInput, kw) {
            hints = append(hints, "检测到显式偏好表达，优先更新用户画像。")
            break
        }
    }

    return len(hints) > 0, hints
}
```

- [ ] **Step 2: Add `buildReflectionPrompt` function**

Add as a package-level function (not a method):

```go
// buildReflectionPrompt constructs a structured fill-in-the-blank reflection prompt.
// hints is an optional list of targeted guidance lines appended after the checklist.
func buildReflectionPrompt(userInput, assistantReply string, hints []string) string {
    var sb strings.Builder
    sb.WriteString("[SELF-GROWTH REFLECTION]\n")
    sb.WriteString("请先调用 search_memory 检索相关记忆，再决定行动。\n\n")
    sb.WriteString("本轮对话摘要（请填写）：\n")
    sb.WriteString("- 用户意图：<一句话>\n")
    sb.WriteString("- 解决方案：<一句话>\n")
    sb.WriteString("- 结果：成功 / 失败 / 部分完成\n\n")
    sb.WriteString("请选择（可不选）：\n")
    sb.WriteString("□ save_memory — 有新的具体事实或偏好值得记录\n")
    sb.WriteString("□ update_user_profile — 发现了用户新的习惯/背景\n")
    sb.WriteString("□ save_skill — 本次解决模式可复用\n")
    if len(hints) > 0 {
        sb.WriteString("\n重点提示：\n")
        for _, h := range hints {
            sb.WriteString("⚠️ ")
            sb.WriteString(h)
            sb.WriteByte('\n')
        }
    }
    sb.WriteString("\n以下是本轮对话内容供参考：\n")
    sb.WriteString("用户：")
    sb.WriteString(userInput)
    sb.WriteString("\n助手：")
    sb.WriteString(assistantReply)
    return sb.String()
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat: add shouldReflect and buildReflectionPrompt for async self-growth"
```

---

### Task 3: Add `reflect` method and wire into `persistAndMigrate`

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add `reflect` method**

Add after `buildReflectionPrompt`:

```go
// reflect runs a background self-growth reflection turn after the main reply.
// It builds a structured prompt and calls ChatDirectCollect; errors are logged
// and discarded — reflection failures must never surface to the user.
func (a *Agent) reflect(ctx context.Context, userInput, assistantReply string, hints []string) {
    defer func() {
        if r := recover(); r != nil {
            slog.Warn("reflect panic recovered", "err", r)
        }
    }()
    prompt := buildReflectionPrompt(userInput, assistantReply, hints)
    if _, err := a.ChatDirectCollect(ctx, prompt); err != nil {
        slog.Warn("self-growth reflection failed", "err", err)
    }
}
```

- [ ] **Step 2: Wire `shouldReflect` + `reflect` into `persistAndMigrate`**

At the end of `persistAndMigrate`, after the `DeleteByIDs` block (and after `a.turnCount.Add(1)`), add the reflection trigger:

```go
// Trigger async self-growth reflection if warranted.
if trigger, hints := a.shouldReflect(userInput, assistantReply); trigger {
    go a.reflect(ctx, userInput, assistantReply, hints)
}
```

The full `persistAndMigrate` signature and the placement: the `turnCount.Add(1)` call is at the top of the function (line 657 in current code). The reflection call goes at the very end, after the `DeleteByIDs` block. If `excess <= 0` returns early, reflection is still skipped — fix this by restructuring `persistAndMigrate` to move the reflection trigger before the early return on `excess <= 0`:

```go
func (a *Agent) persistAndMigrate(ctx context.Context, userInput string, userImages []string, userFiles []string, assistantReply string) {
    if a.shortMem == nil {
        return
    }

    a.turnCount.Add(1)

    if _, err := a.shortMem.AddWithImagesAndFiles("user", userInput, userImages, userFiles); err != nil {
        slog.Error("save user message failed", "err", err)
        return
    }
    if _, err := a.shortMem.Add("assistant", assistantReply); err != nil {
        slog.Error("save assistant message failed", "err", err)
        return
    }

    // Trigger async self-growth reflection if warranted.
    if trigger, hints := a.shouldReflect(userInput, assistantReply); trigger {
        go a.reflect(ctx, userInput, assistantReply, hints)
    }

    limit := a.cfg.ShortTermLimit
    if limit <= 0 {
        limit = 30
    }

    count, err := a.shortMem.Count()
    if err != nil {
        slog.Error("count messages failed", "err", err)
        return
    }

    excess := count - limit
    if excess <= 0 {
        return
    }

    oldest, err := a.shortMem.OldestN(excess)
    if err != nil {
        slog.Error("get oldest messages failed", "err", err)
        return
    }
    if len(oldest) == 0 {
        return
    }

    if a.longMem != nil {
        block := memory.FormatBlock(oldest)
        if err := a.longMem.Store(ctx, block); err != nil {
            slog.Error("store long-term memory failed", "err", err)
        }
    }

    ids := make([]int64, len(oldest))
    for i, m := range oldest {
        ids[i] = m.ID
    }
    if err := a.shortMem.DeleteByIDs(ids); err != nil {
        slog.Error("delete migrated messages failed", "err", err)
    }
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat: wire async self-growth reflection into persistAndMigrate"
```

---

### Task 4: Update `Chat()` and `ChatWithMessage()` to pass `assistantReply` into `persistAndMigrate`

**Files:**
- Modify: `internal/agent/agent.go`

> **Note:** After Task 1, `persistAndMigrate` is already called with `fullResponse`. Verify the call signatures match — `persistAndMigrate(ctx, userInput, nil, nil, fullResponse)` in `Chat()` and `persistAndMigrate(ctx, userMemory, userImages, userFiles, fullResponse)` in `ChatWithMessage()`. Both already pass `fullResponse` as `assistantReply`, so no change is needed here. This task is a verification step only.

- [ ] **Step 1: Verify `Chat()` passes `fullResponse` to `persistAndMigrate`**

Check line in `Chat()`:
```go
go a.persistAndMigrate(context.Background(), userInput, nil, nil, fullResponse)
```
Confirm `fullResponse` is the 5th argument (maps to `assistantReply`). ✓

- [ ] **Step 2: Verify `ChatWithMessage()` passes `fullResponse` to `persistAndMigrate`**

Check line in `ChatWithMessage()`:
```go
go a.persistAndMigrate(context.Background(), userMemory, userImages, userFiles, fullResponse)
```
Confirm `fullResponse` is the 5th argument. ✓

- [ ] **Step 3: Run full build**

```bash
cd /Users/xutiancheng/code/self/Aiko && go build ./...
```

Expected: no errors.

---

### Task 5: Manual smoke test

**Files:** none

- [ ] **Step 1: Build and launch**

```bash
cd /Users/xutiancheng/code/self/Aiko && make run
```

- [ ] **Step 2: Send 5 messages and verify no nudge text leaks into user-visible replies**

Have a normal conversation for 5 turns. The 5th turn should NOT show nudge text inside the assistant's reply to you. The reflection runs in the background.

- [ ] **Step 3: Check logs for reflection activity**

After the 5th turn, wait 5–10 seconds and check the console output. You should see either:
- No `reflect` log lines (model chose to skip all three tools — valid)
- Or tool-call logs for `search_memory`, `save_memory`, `update_user_profile`, or `save_skill`

You should NOT see:
- `self-growth reflection failed` with an error (unless the API key is invalid)
- `reflect panic recovered`

- [ ] **Step 4: Test correction keyword trigger**

Send: "不对，你刚才说的XXX是错的". This should trigger an immediate reflection (not wait for the nudgeInterval). Check logs.

- [ ] **Step 5: Commit final state**

```bash
git add internal/agent/agent.go
git commit -m "feat: complete async self-growth reflection system"
```
