# Agent Rebuild Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the duplicated tool-list-building + `agent.New` call shared by `initLLMComponents` and `rebuildAgentTools` into a private `buildAgent` helper, and delete the dead `chatFn` in `rebuildAgentTools`.

**Architecture:** One new private method `buildAgent` in `app.go`. Both callers pass their already-resolved components; `buildAgent` handles `AllEino`, `AllContextual`, `followupTool`, skills, middleware, and `agent.New`. No behavior changes.

**Tech Stack:** Go, `internal/agent`, `internal/tools`, `internal/skill`, `internal/middleware`.

---

## File Map

| File | Changes |
|------|---------|
| `app.go` | Add `buildAgent` (~50 lines); simplify `initLLMComponents` (~40 lines removed); simplify `rebuildAgentTools` (~45 lines removed, including dead chatFn) |

---

### Task 1: Add `buildAgent` and simplify both callers

This is one atomic task — all three changes must compile together.

**Files:**
- Modify: `app.go` — `initLLMComponents` (line ~492), `rebuildAgentTools` (line ~760)

- [ ] **Step 1: Read the current state of both methods**

  Read `app.go` lines 492–861 to confirm exact text before editing. Pay attention to:
  - The `builtinTools` / `contextTools` / `followupTool` / `allTools` block in `initLLMComponents` (roughly lines 532–661)
  - The `builtinTools` / `chatFn` / `contextTools` / `followupTool` / `allTools` block in `rebuildAgentTools` (roughly lines 775–820)
  - The `skillDirs` / `skillMW` block in both methods
  - The `mw` / `agent.New` block in both methods

- [ ] **Step 2: Insert `buildAgent` immediately before `initLLMComponents`**

  Find the line `func (a *App) initLLMComponents(ctx context.Context) error {` and insert the following new method on the lines immediately preceding it (before the blank line and doc comment):

  ```go
  // buildAgent assembles the full tool list and constructs a new eino Agent from
  // already-resolved components. extraTools is nil for the initial build and
  // contains MCP tools for subsequent rebuilds.
  func (a *App) buildAgent(
  	ctx context.Context,
  	chatModel eino.ChatModel,
  	longMem *memory.LongStore,
  	knowledgeSt *knowledge.Store,
  	sched *scheduler.Scheduler,
  	extraTools []tool.BaseTool,
  	cfgSkillsDirs []string,
  ) (*agent.Agent, error) {
  	dataDir := a.dataDir
  	builtinTools := internaltools.AllEino(a.permStore)
  	contextTools := internaltools.AllContextual(
  		a.permStore,
  		knowledgeSt,
  		sched,
  		longMem,
  		dataDir,
  		a.cfg,
  		func(id string, cancel func()) {
  			a.runningCmds.Store(id, cancel)
  			wailsruntime.EventsEmit(a.ctx, "tool:executing", map[string]any{"id": id})
  		},
  		func(id string) {
  			a.runningCmds.Delete(id)
  			wailsruntime.EventsEmit(a.ctx, "tool:executed", map[string]any{"id": id})
  		},
  		func() { go a.initLLMComponents(a.ctx) },
  		a.InstallUpdate,
  		func(event string, data any) { wailsruntime.EventsEmit(a.ctx, event, data) },
  		version,
  	)
  	proactiveStore := proactive.NewStore(a.sqlDB)
  	followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), a.permStore)
  	allTools := append(builtinTools, contextTools...)
  	allTools = append(allTools, extraTools...)
  	allTools = append(allTools, followupTool)

  	autoSkillsDir := filepath.Join(dataDir, "auto-skills")
  	skillDirs := append(append([]string{}, cfgSkillsDirs...), autoSkillsDir)
  	skillMW, err := skill.NewMiddleware(ctx, skillDirs)
  	if err != nil {
  		return nil, fmt.Errorf("load skills: %w", err)
  	}

  	mw := middleware.Chain(
  		middleware.Logging(),
  		middleware.Retry(3, 200*time.Millisecond),
  		middleware.ErrorRecovery(),
  	)

  	return agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW, dataDir,
  		&a.pendingConfirms,
  		func(event string, data ...any) {
  			wailsruntime.EventsEmit(a.ctx, event, data...)
  		},
  	)
  }
  ```

  Note on the `eino.ChatModel` type: look at how it is referenced in the existing `initLLMComponents` function to confirm the correct import path/type name. It is the type returned by `llm.NewChatModel`. Check the existing field declaration `a.chatModel` in the `App` struct to see the exact type.

- [ ] **Step 3: In `initLLMComponents`, replace the tool-building + agent.New block**

  Find and replace this block in `initLLMComponents` (the section starting with `// Built-in tools + context-aware tools`):

  ```go
  	// Built-in tools + context-aware tools (knowledge) + skill tools
  	builtinTools := internaltools.AllEino(a.permStore)

  	// Build a chat function for the scheduler.
  	// When SaveToMemory is true the job uses ag.Chat() so the exchange goes
  	// through persistAndMigrate (identical to typing in the chat panel) AND
  	// streams tokens to the chat panel via chat:cron:start / chat:token / chat:done.
  	// Otherwise ChatDirect is used to avoid polluting short/long-term memory.
  	chatFn := func(ctx context.Context, job scheduler.Job) (string, error) {
  		a.mu.RLock()
  		ag := a.petAgent
  		a.mu.RUnlock()
  		if ag == nil {
  			return "", fmt.Errorf("agent not ready")
  		}
  		var ch <-chan agent.StreamResult
  		if job.SaveToMemory {
  			// Signal the chat panel to open a streaming assistant bubble and
  			// show the cron job prompt as the user-side trigger message.
  			wailsruntime.EventsEmit(a.ctx, "chat:cron:start", map[string]any{
  				"name":   job.Name,
  				"prompt": job.Prompt,
  			})
  			ch = ag.Chat(ctx, job.Prompt)
  		} else {
  			ch = ag.ChatDirect(ctx, job.Prompt)
  		}
  		ep := agent.NewEmotionParser()
  		var sb strings.Builder
  		for r := range ch {
  			if r.Err != nil {
  				if job.SaveToMemory {
  					wailsruntime.EventsEmit(a.ctx, "chat:done", "")
  				}
  				return "", r.Err
  			}
  			if job.SaveToMemory && len(r.Images) > 0 {
  				wailsruntime.EventsEmit(a.ctx, "chat:image", r.Images)
  			}
  			if r.Done {
  				break
  			}
  			if r.ThinkingToken != "" {
  				if job.SaveToMemory {
  					wailsruntime.EventsEmit(a.ctx, "chat:thinking", r.ThinkingToken)
  				}
  				continue
  			}
  			text, _, _ := ep.Feed(r.Token)
  			sb.WriteString(text)
  			if job.SaveToMemory && text != "" {
  				wailsruntime.EventsEmit(a.ctx, "chat:token", text)
  			}
  		}
  		// Flush any buffered tail after the last token.
  		if tail := ep.Flush(); tail != "" {
  			sb.WriteString(tail)
  			if job.SaveToMemory {
  				wailsruntime.EventsEmit(a.ctx, "chat:token", tail)
  			}
  		}
  		if job.SaveToMemory {
  			wailsruntime.EventsEmit(a.ctx, "chat:done", "")
  		}
  		return sb.String(), nil
  	}

  	onResult := func(job scheduler.Job, result string, err error) {
  		... (entire onResult closure)
  	}

  	// NOTE: scheduler.Start is deferred until after a.mu is released below —
  	// cron callbacks emit Wails events which may reenter Go, and emitting while
  	// holding a.mu can deadlock if the event handler calls an App method.
  	sched := scheduler.New(a.sqlDB, chatFn, onResult)

  	contextTools := internaltools.AllContextual(
  		...
  	)
  	proactiveStore := proactive.NewStore(a.sqlDB)
  	followupTool := internaltools.ToEino(proactive.NewScheduleFollowupTool(proactiveStore), a.permStore)
  	// MCP tools are loaded asynchronously after the agent is online (see below).
  	allTools := append(builtinTools, contextTools...)
  	allTools = append(allTools, followupTool)

  	// Build skill middleware from configured directories.
  	autoSkillsDir := filepath.Join(dataDir, "auto-skills")
  	skillDirs := append(append([]string{}, cfg.SkillsDirs...), autoSkillsDir)
  	skillMW, err := skill.NewMiddleware(ctx, skillDirs)
  	if err != nil {
  		return fmt.Errorf("load skills: %w", err)
  	}

  	// Middleware chain: logging -> retry -> error recovery (outermost first)
  	mw := middleware.Chain(
  		middleware.Logging(),
  		middleware.Retry(3, 200*time.Millisecond),
  		middleware.ErrorRecovery(),
  	)

  	newAgent, err := agent.New(ctx, chatModel, a.shortMem, longMem, allTools, a.cfg, mw, skillMW, dataDir,
  		&a.pendingConfirms,
  		func(event string, data ...any) {
  			wailsruntime.EventsEmit(a.ctx, event, data...)
  		},
  	)
  	if err != nil {
  		return fmt.Errorf("new agent: %w", err)
  	}
  ```

  **IMPORTANT**: Keep `chatFn`, `onResult`, and `sched := scheduler.New(...)` intact — these are NOT being extracted. Only replace the section starting after `sched := scheduler.New(...)` through the `newAgent, err := agent.New(...)` block.

  The replacement for that section is:

  ```go
  	newAgent, err := a.buildAgent(ctx, chatModel, longMem, knowledgeSt, sched, nil, cfg.SkillsDirs)
  	if err != nil {
  		return fmt.Errorf("build agent: %w", err)
  	}
  ```

  This replaces everything from `contextTools := internaltools.AllEino(...)` through (and including) the `if err != nil { return fmt.Errorf("new agent: %w", err) }` block.

- [ ] **Step 4: In `rebuildAgentTools`, remove dead chatFn and replace tool/agent block**

  In `rebuildAgentTools`, find and remove the entire `chatFn` local variable declaration (the one that builds a simple `ChatDirect` loop). It currently ends with `_ = chatFn`.

  Then replace the section from `contextTools := internaltools.AllContextual(...)` through (and including) `if err != nil { return fmt.Errorf("new agent: %w", err) }` with:

  ```go
  	newAgent, err := a.buildAgent(ctx, chatModel, longMem, knowledgeSt, sched, mcpTools, cfgSnapshot.SkillsDirs)
  	if err != nil {
  		return fmt.Errorf("build agent: %w", err)
  	}
  ```

- [ ] **Step 5: Verify build**

  ```bash
  go build ./...
  ```

  If there are type errors about `eino.ChatModel`, check the `App` struct field declaration for `chatModel` and use the same type. Also verify the `agent.New` function signature hasn't changed.

- [ ] **Step 6: Verify race-free build**

  ```bash
  go build -race ./...
  ```

  Expected: exits 0.

- [ ] **Step 7: Commit**

  ```bash
  git add app.go
  git commit -m "refactor: extract buildAgent helper, eliminate initLLMComponents/rebuildAgentTools duplication"
  ```

---

### Task 2: Final verification

**Files:** none modified

- [ ] **Step 1: Verify `AllContextual` call appears exactly once**

  ```bash
  grep -c "AllContextual" app.go
  ```

  Expected: `1` (only in `buildAgent`).

- [ ] **Step 2: Verify dead chatFn is gone from rebuildAgentTools**

  ```bash
  grep -n "_ = chatFn" app.go
  ```

  Expected: no output.

- [ ] **Step 3: Verify `agent.New` call appears exactly once**

  ```bash
  grep -c "agent\.New" app.go
  ```

  Expected: `1` (only in `buildAgent`).
