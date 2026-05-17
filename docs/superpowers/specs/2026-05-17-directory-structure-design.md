# Directory Structure Optimization Design

## Goal

Split two oversized code areas into well-bounded, navigable units:

1. `internal/tools/` — 48 files in a single flat package → 20 domain sub-packages
2. `internal/agent/agent.go` — 1400-line monolith → 4 focused files in the same package

Both changes are zero-behavior-change refactors: no logic modifications, only reorganization.

---

## Sub-project 1: `internal/tools/` Sub-package Split

### Problem

The `internal/tools/` package is a monolith: 48 files, ~7000 lines, all in `package tools`. The largest files (`dev_tools.go` at 1229 lines, `web_tools.go` at 647 lines, `growth_tools.go` at 402 lines) each contain multiple unrelated tool types. Navigation requires scanning dozens of files with no structural grouping.

### Architecture

**Circular import constraint:** Go sub-packages cannot import their parent package. Since all tool types implement the `Tool` interface (currently in `tool.go`) and reference `PermissionLevel` constants, these base types must move to `internal/tools/base` — a leaf package with no internal dependencies. Both the parent registry and all sub-packages import `base`; no cycles.

**Registry pattern:** `registry.go` stays in `internal/tools/`. Each sub-package exports a slice or registration function that `registry.go` aggregates into `All()`, `AllEino()`, `AllContextual()`, and `AllPermissionDeclarations()`.

### Target Structure

```
internal/tools/
├── base/               # Tool interface, PermissionLevel, parseArgs (~30 lines, no internal deps)
├── permission.go       # PermissionStore (unchanged)
├── registry.go         # All, AllEino, AllContextual, AllPermissionDeclarations (aggregation only)
│
├── dev/                # Developer utilities
│   ├── dev_json.go         # FormatJSON, JSONToStruct, YAMLJSONConvert
│   ├── dev_encode.go       # EncodeDecode, HashText
│   ├── dev_convert.go      # ConvertTimestamp, NumberBaseConvert, ConvertUnits, ConvertColor, GetExchangeRate
│   ├── dev_misc.go         # GenerateUUID, RegexTest
│   └── dev_tools_test.go
├── web/                # WebSearch, WebFetch
│   ├── web_tools.go
│   └── web_tools_test.go
├── fs/                 # Filesystem operations (ListDirectory, ReadFile, WriteFile, Delete, MakeDir, Move)
│   ├── filesystem.go       # Implementation helpers
│   └── filesystem_tools.go # Tool structs
├── exec/               # Shell + code execution (both require eino interrupt/resume)
│   ├── shell.go            # Shell execution logic
│   ├── shell_tools.go      # Shell tool struct
│   ├── code.go             # Code execution logic
│   ├── code_tools.go       # Code tool struct
│   └── shell_test.go
├── growth/             # Self-growth: memory, skills, user profile
│   └── growth_tools.go     # SaveMemory, SearchMemory, UpdateUserProfile, ListSkills, SaveSkill
├── system/             # OS/hardware info + app update
│   ├── system_tools.go     # GetOSInfo, GetHardwareInfo, GetSystemStats, GetNetworkStatus
│   ├── update_tools.go
│   ├── update_darwin.go
│   └── update_other.go
├── image/              # Image processing
│   ├── image.go
│   └── image_tools.go
├── location/           # Geolocation
│   ├── location_tools.go
│   ├── location_darwin.go
│   ├── location_other.go
│   └── location_geocode.go
├── browser/            # Browser URL/content awareness
│   ├── browser_tools.go
│   ├── browser_darwin.go
│   └── browser_other.go
├── clipboard/          # Clipboard read/write
│   ├── clipboard_tools.go
│   ├── clipboard_darwin.go
│   └── clipboard_other.go
├── calendar/           # macOS Calendar read/write
│   ├── calendar_tools.go
│   ├── calendar_darwin.go
│   └── calendar_other.go
├── reminders/          # macOS Reminders
│   ├── reminders_darwin.go
│   └── reminders_other.go
├── mail/               # macOS Mail reading
│   ├── mail_darwin.go
│   └── mail_other.go
├── screenshot/         # Screen capture
│   ├── screenshot_tools.go
│   ├── screenshot_darwin.go
│   └── screenshot_other.go
├── appctl/             # App control (activate/quit)
│   ├── app_control_tools.go
│   ├── app_control_darwin.go
│   └── app_control_other.go
├── weather/            # Weather lookup
│   └── weather_tools.go
├── timeutil/           # Time tools (named to avoid stdlib `time` conflict)
│   └── time_tools.go
├── context/            # User context injection tools
│   └── context_tools.go
└── cron/               # Cron job management tools (distinct from internal/scheduler)
    └── scheduler_tools.go
```

### File Count After

| Location | Before | After |
|---|---|---|
| `internal/tools/` (flat) | 48 files | 3 files (base types + permission + registry) |
| Sub-packages total | — | ~50 files across 20 packages |
| Largest single file | 1229 lines | ~300 lines |

### Callers

External callers of `internal/tools` (primarily `app.go` and `internal/agent/`) import only:
- `internal/tools` — for `PermissionStore`, `AllEino()`, `AllContextual()`, `AllPermissionDeclarations()`
- `internal/tools/base` — for the `Tool` interface type if referenced directly

No other packages need to import individual sub-packages; the registry aggregates everything.

---

## Sub-project 2: `internal/agent/agent.go` Split

### Problem

`agent.go` is ~1400 lines containing four logically distinct responsibilities mixed in one file: struct construction, chat entry points, eino stream draining, and context building. After sub-project 3 refactoring three helper functions were already extracted, but the file is still unwieldy.

### Architecture

Same-package split (`package agent`). No import path changes anywhere. All types remain accessible across files. Risk is minimal — this is pure file reorganization.

### Target Structure

```
internal/agent/
├── agent.go        # Agent struct, fields, constants, New, buildAgentRunner,
│                   # helper types (StreamResult, memCheckPointStore, locationCache) (~250 lines)
├── chat.go         # Chat, ChatDirect, ChatDirectCollect, ChatWithMessage, SetSkillHint (~350 lines)
├── drain.go        # drainIter, drainIterInner, processStreamingMessage, handleInterrupt (~400 lines)
├── context.go      # buildContext, gatherContextSources, persistAndMigrate, readUserProfile (~300 lines)
├── emotion.go      # EmotionParser (unchanged)
├── middleware/     # Middleware sub-package (unchanged)
├── agent_test.go   # (unchanged)
└── emotion_test.go # (unchanged)
```

### Responsibility Boundaries

| File | Answers the question |
|---|---|
| `agent.go` | What is this type and how is it constructed? |
| `chat.go` | What does the caller invoke? |
| `drain.go` | How are eino async events processed internally? |
| `context.go` | How is conversation context assembled before each turn? |

### What Stays in agent.go

- `Agent` struct and all field declarations
- `StreamResult`, `memCheckPointStore`, `locationCache`, `streamResultBufSize`
- `New` and `buildAgentRunner` (construction belongs with the struct)
- `emotionPromptSuffix` constant

---

## Implementation Order

1. **Sub-project 2 first** (agent split) — smaller scope, zero import changes, fast to verify
2. **Sub-project 1 second** (tools split) — larger scope, requires updating all callers of moved types

Each sub-project gets its own implementation plan and commit sequence.

## Testing Strategy

- `go build ./...` must pass after every commit
- `go test ./...` must pass after each sub-project completes
- No test logic changes — only package paths in test imports may change (for tools sub-project)
