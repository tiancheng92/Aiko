# Agent Self-Growth Efficiency — Design Spec

**Date:** 2026-05-05  
**Scope:** `internal/agent/agent.go` only (Approach A — minimal invasion)  
**Status:** Approved

---

## Problem

The current self-growth nudge is:
- **Synchronous** — injected into the user message before the reply, adding latency and polluting the conversation context
- **Periodic only** — fires every N turns regardless of whether anything meaningful happened
- **Open-ended** — asks the model to "consider whether..." with no structure, leading to inconsistent follow-through
- **Blind** — no search before save, so duplicate memories accumulate
- **Skill-unaware** — when a skill is activated by middleware, there's no feedback loop to update it

---

## Design

### Approach A: All changes in `agent.go`, no new files

All 5 optimizations are implemented in `internal/agent/agent.go`. No new packages, no new files.

---

## 1. Async Post-Reply Reflection

**Current behavior:** nudge text is appended to `userInput` in `Chat()` / `ChatWithMessage()` before the agent replies.

**New behavior:** nudge is triggered inside the `persistAndMigrate` goroutine, _after_ the reply is fully streamed and saved. A separate `ChatDirectCollect` call runs the reflection prompt as a background task.

**Changes:**
- Remove nudge injection from `Chat()` and `ChatWithMessage()`
- Add `reflect(ctx, userInput, assistantReply string, hints []string)` method called from `persistAndMigrate`
- `reflect` builds the prompt via `buildReflectionPrompt`, then calls `ChatDirectCollect`; errors are logged and discarded

---

## 2. Event-Driven Triggers

**New method:** `shouldReflect(userInput, assistantReply string) (bool, []string)`

Returns `(trigger bool, hints []string)`. Trigger is true under **any** of:

| Condition | Detection | Hint appended |
|-----------|-----------|---------------|
| Periodic | `turnCount % nudgeInterval == 0` | none (generic prompt) |
| User correction | userInput contains "不对" / "你刚才说错了" / "其实是" / "纠正" | "检查相关 skill 是否需要更新" |
| Multi-tool chain | assistantReply tool-call markers ≥ 3 | "本次涉及 N 个工具调用，考虑提炼为技能" |
| Explicit preference | userInput contains "以后都" / "我不喜欢" / "记住" / "每次都" | "优先更新用户画像" |

Signal detection uses simple `strings.Contains` — no regex, no NLP.

---

## 3. Search Before Save

**New behavior:** the reflection prompt explicitly instructs the agent to call `search_memory` before deciding to save, and to update an existing memory rather than append if similarity is high.

This is prompt-level, no code change to memory layer needed (deduplication at storage layer already implemented separately).

---

## 4. Structured Nudge Prompt

**New function:** `buildReflectionPrompt(userInput, assistantReply string, hints []string) string`

Output format (fill-in-the-blank):

```
[SELF-GROWTH REFLECTION]
请先调用 search_memory 检索相关记忆，再决定行动。

本轮对话摘要：
- 用户意图：<一句话>
- 解决方案：<一句话>
- 结果：成功 / 失败 / 部分完成

请选择（可不选）：
□ save_memory — 有新的具体事实或偏好值得记录
□ update_user_profile — 发现了用户新的习惯/背景
□ save_skill — 本次解决模式可复用

{hints}
```

If `hints` is empty, the hints section is omitted.

---

## 5. Skill Usage Feedback Loop

**New field:** `Agent.hasSkills bool` — set to `true` at startup if any skill files exist in `dataDir/auto-skills/`.

**New field:** `Agent.lastSkillHint string` — written by the skill middleware when a skill is activated; read and cleared by `persistAndMigrate` when building hints.

**Flow:**
1. Skill middleware activates a skill named `{name}` → sets `a.lastSkillHint = "本次激活了 skill \"{name}\"，回复结束后思考该 skill 是否需要更新。"`
2. `persistAndMigrate` reads `a.lastSkillHint`, appends to hints, clears it
3. `shouldReflect` returns true whenever hints is non-empty (even if periodic condition not met)

**Concurrency:** `lastSkillHint` is protected by a `sync.Mutex` (separate from `a.mu`).

---

## Struct Changes

```go
type Agent struct {
    // existing fields ...
    hasSkills     bool
    lastSkillHint string
    skillHintMu   sync.Mutex
}
```

---

## Data Flow

```
Chat() / ChatWithMessage()
  └─ stream reply to frontend
  └─ go persistAndMigrate(userInput, assistantReply)
        ├─ save short-term messages
        ├─ migrate overflow to long-term memory
        ├─ increment turnCount
        ├─ read & clear lastSkillHint
        ├─ shouldReflect(userInput, assistantReply) → (bool, hints)
        └─ if true: go reflect(ctx, userInput, assistantReply, hints)
                        └─ buildReflectionPrompt(...)
                        └─ ChatDirectCollect(...)  [background, errors discarded]
```

---

## What Does NOT Change

- `ChatDirect` / `ChatDirectCollect` — no nudge injected (used by scheduler/proactive engine)
- `memory` package — no changes (dedup already implemented)
- `tools` package — no changes
- Frontend — no changes
- Database schema — no changes

---

## Error Handling

- `reflect()` errors are logged via `slog.Warn`, never surfaced to the user
- If `ChatDirectCollect` panics inside reflection, it is recovered silently
- `lastSkillHint` is always cleared after reading, even if reflection is skipped
