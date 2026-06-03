# VRM Behavior System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade VRM pet from emotion-tag-only blendshape control to a unified behavior tag system that drives both sustained animations and one-shot gestures, while reverting Live2D's emotion support.

**Architecture:** Backend: `BehaviorParser` replaces `EmotionParser`, parsing `[表现:emotion]` / `[表现:emotion,动作:action]` tags from the LLM token stream, emitting `chat:behavior` event. Frontend: VRMPet switches to `chat:behavior`, unifying emotion/animation/action dispatch under a `behavior > emotion > petState` priority model.

**Tech Stack:** Go (eino ReAct Agent), Vue 3 Composition API, Three.js VRM

---

### Task 1: Rename emotion.go → behavior.go, rewrite BehaviorParser

**Files:**
- Create: `internal/agent/behavior.go`
- Delete: `internal/agent/emotion.go`

- [ ] **Step 1: Write behavior.go with BehaviorParser**

Write: `internal/agent/behavior.go`

```go
package agent

import (
	"regexp"
	"strings"
)

var behaviorTagRe = regexp.MustCompile(`^\[表现:(\w+)(?:,动作:(\w+))?\]\n?`)

// parseBehaviorTag extracts a behavior tag from the start of s.
// On success returns (emotion, action, textAfterTag, true).
// action is empty string when no action is specified.
// On failure returns ("", "", s, false) — original string preserved.
func parseBehaviorTag(s string) (string, string, string, bool) {
	m := behaviorTagRe.FindStringSubmatchIndex(s)
	if m == nil {
		return "", "", s, false
	}
	emotion := s[m[2]:m[3]]
	// action is optional capture group (index 4-5), may be -1 if absent.
	var action string
	if m[4] >= 0 {
		action = s[m[4]:m[5]]
	}
	return emotion, action, s[m[1]:], true
}

// BehaviorParser is a per-response streaming state machine that extracts the
// optional behavior prefix tag from the token stream.
// It is not safe for concurrent use.
type BehaviorParser struct {
	parsing bool
	buf     strings.Builder
}

// NewBehaviorParser returns a fresh parser ready for a new assistant response.
func NewBehaviorParser() *BehaviorParser {
	return &BehaviorParser{parsing: true}
}

// Feed processes one incoming token.
// Returns (textToEmit, emotion, action).
//   - textToEmit is non-empty when text should be forwarded to the display.
//   - emotion is non-empty when a behavior tag was detected.
//   - action may be empty even when emotion is set (no action specified).
func (p *BehaviorParser) Feed(tok string) (text string, emotion string, action string) {
	if !p.parsing {
		return tok, "", ""
	}
	p.buf.WriteString(tok)
	s := p.buf.String()

	// Try to parse a complete tag when we see the closing bracket.
	if strings.Contains(s, "]") {
		em, act, rest, ok := parseBehaviorTag(s)
		if ok {
			p.parsing = false
			p.buf.Reset()
			return rest, em, act
		}
		// Has ] but doesn't match — give up, flush.
		p.parsing = false
		p.buf.Reset()
		return s, "", ""
	}

	// Buffer exceeds 60 bytes without seeing ] — give up.
	if p.buf.Len() > 60 {
		p.parsing = false
		p.buf.Reset()
		return s, "", ""
	}

	// Still accumulating — withhold from display.
	return "", "", ""
}

// Flush returns any remaining buffered text (call on stream end).
func (p *BehaviorParser) Flush() string {
	if !p.parsing || p.buf.Len() == 0 {
		return ""
	}
	s := p.buf.String()
	p.buf.Reset()
	p.parsing = false
	return s
}
```

- [ ] **Step 2: Delete emotion.go**

Run: `rm internal/agent/emotion.go`

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/xutiancheng/code/self/Aiko && go build ./internal/agent/`
Expected: compilation errors from callers still referencing EmotionParser (will fix in subsequent tasks)

- [ ] **Step 4: Commit**

```bash
git add internal/agent/behavior.go
git rm internal/agent/emotion.go
git commit -m "refactor(agent): rename EmotionParser to BehaviorParser for unified behavior tags"
```

---

### Task 2: Update tests

**Files:**
- Create: `internal/agent/behavior_test.go`
- Delete: `internal/agent/emotion_test.go`

- [ ] **Step 1: Write behavior_test.go**

Write: `internal/agent/behavior_test.go`

```go
package agent

import (
	"testing"
)

func TestParseBehaviorTag(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		emotion string
		action  string
		rest    string
		ok      bool
	}{
		{"emotion only", "[表现:joy]\n你好", "joy", "", "你好", true},
		{"emotion with action", "[表现:joy,动作:wave]\n你好", "joy", "wave", "你好", true},
		{"neutral", "[表现:neutral]\n", "neutral", "", "", true},
		{"sad with action", "[表现:sad,动作:nod]\n文字", "sad", "nod", "文字", true},
		{"no tag", "普通回复", "", "", "普通回复", false},
		{"old format", "[情绪:joy/0.8]\n你好", "", "", "[情绪:joy/0.8]\n你好", false},
		{"partial tag", "[表现:joy", "", "", "[表现:joy", false},
		{"missing bracket", "表现:joy]\n", "", "", "表现:joy]\n", false},
		{"empty", "", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emotion, action, rest, ok := parseBehaviorTag(tc.input)
			if ok != tc.ok {
				t.Errorf("ok: got %v want %v", ok, tc.ok)
			}
			if ok {
				if emotion != tc.emotion {
					t.Errorf("emotion: got %q want %q", emotion, tc.emotion)
				}
				if action != tc.action {
					t.Errorf("action: got %q want %q", action, tc.action)
				}
				if rest != tc.rest {
					t.Errorf("rest: got %q want %q", rest, tc.rest)
				}
			} else {
				if rest != tc.input {
					t.Errorf("on fail, rest should equal input: got %q want %q", rest, tc.input)
				}
			}
		})
	}
}

func TestBehaviorParserStreaming(t *testing.T) {
	p := NewBehaviorParser()
	tokens := []string{"[表现:", "joy,动作:", "wave]\n", "你好世界"}
	var emittedEmotion string
	var emittedAction string
	var emittedTokens []string

	for _, tok := range tokens {
		text, emotion, action := p.Feed(tok)
		if text != "" {
			emittedTokens = append(emittedTokens, text)
		}
		if emotion != "" {
			emittedEmotion = emotion
			emittedAction = action
		}
	}
	if emittedEmotion != "joy" {
		t.Errorf("emotion: got %q want joy", emittedEmotion)
	}
	if emittedAction != "wave" {
		t.Errorf("action: got %q want wave", emittedAction)
	}
	if len(emittedTokens) != 1 || emittedTokens[0] != "你好世界" {
		t.Errorf("tokens: got %v want [你好世界]", emittedTokens)
	}
}

func TestBehaviorParserStreamingNoAction(t *testing.T) {
	p := NewBehaviorParser()
	tokens := []string{"[表现:", "sad]\n", "抱歉"}
	var emittedEmotion string
	var emittedAction string
	var emittedTokens []string

	for _, tok := range tokens {
		text, emotion, action := p.Feed(tok)
		if text != "" {
			emittedTokens = append(emittedTokens, text)
		}
		if emotion != "" {
			emittedEmotion = emotion
			emittedAction = action
		}
	}
	if emittedEmotion != "sad" {
		t.Errorf("emotion: got %q want sad", emittedEmotion)
	}
	if emittedAction != "" {
		t.Errorf("action: got %q want empty", emittedAction)
	}
	if len(emittedTokens) != 1 || emittedTokens[0] != "抱歉" {
		t.Errorf("tokens: got %v want [抱歉]", emittedTokens)
	}
}

func TestBehaviorParserFallback(t *testing.T) {
	p := NewBehaviorParser()
	long := "[这是一段超过六十个字节的普通文字不是行为标签，纯粹是正文内容"
	text, emotion, _ := p.Feed(long)
	if emotion != "" {
		t.Errorf("should not emit emotion for long non-tag: got %q", emotion)
	}
	if text != long {
		t.Errorf("should flush buffer as text: got %q want %q", text, long)
	}
}

func TestBehaviorParserFlush(t *testing.T) {
	p := NewBehaviorParser()
	text, _, _ := p.Feed("[表现:joy")
	if text != "" {
		t.Errorf("should buffer partial tag, got %q", text)
	}
	tail := p.Flush()
	if tail != "[表现:joy" {
		t.Errorf("Flush: got %q want %q", tail, "[表现:joy")
	}
}
```

- [ ] **Step 2: Delete emotion_test.go**

Run: `rm internal/agent/emotion_test.go`

- [ ] **Step 3: Run tests**

Run: `cd /Users/xutiancheng/code/self/Aiko && go test ./internal/agent/ -v -run "TestParseBehaviorTag|TestBehaviorParser"`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agent/behavior_test.go
git rm internal/agent/emotion_test.go
git commit -m "test(agent): update tests for BehaviorParser with behavior tag format"
```

---

### Task 3: Update system prompt in agent.go

**Files:**
- Modify: `internal/agent/agent.go:23-27` (emotionPromptSuffix)
- Modify: `internal/agent/agent.go:146` (usage site)

- [ ] **Step 1: Replace emotionPromptSuffix with behaviorPromptSuffix**

Edit `internal/agent/agent.go`:

Old (lines 23-27):
```go
const emotionPromptSuffix = "\n\n在每条回复的第一行必须输出情绪标签，格式严格为 `[情绪:emotion/intensity]`，" +
	"其中 emotion ∈ {joy, sad, surprised, angry, neutral}，intensity ∈ [0.0, 1.0]，然后换行写正文。" +
	"示例：[情绪:joy/0.7]\n你好！"
```

New:
```go
const behaviorPromptSuffix = "\n\n在每条回复的第一行必须输出行为标签，格式为 `[表现:emotion]` 或 `[表现:emotion,动作:action]`。" +
	"emotion ∈ {joy, sad, surprised, angry, neutral}。" +
	"action ∈ {wave, nod, celebrate, surprised_react} 为可选项，表示一次性手势。" +
	"然后换行写正文。示例：[表现:joy,动作:wave]\n你好！"
```

- [ ] **Step 2: Update usage site**

Edit `internal/agent/agent.go` line 146:

Old:
```go
systemPrompt := cfg.SystemPrompt + toolPolicyPrompt + emotionPromptSuffix
```

New:
```go
systemPrompt := cfg.SystemPrompt + toolPolicyPrompt + behaviorPromptSuffix
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/xutiancheng/code/self/Aiko && go build ./internal/agent/`
Expected: compile success (callers still reference EmotionParser — will fix in Task 4)

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "refactor(agent): update system prompt for behavior tag format"
```

---

### Task 4: Update app_chat.go drain loop

**Files:**
- Modify: `internal/agent/context.go:252` (emotion tag stripping)
- Modify: `app_chat.go:118-151` (drain loop)

- [ ] **Step 1: Update context.go tag stripping**

Edit `internal/agent/context.go` line 252:

Old:
```go
if _, _, stripped, ok := parseEmotionTag(assistantReply); ok {
```

New:
```go
if _, _, stripped, ok := parseBehaviorTag(assistantReply); ok {
```

- [ ] **Step 2: Update app_chat.go drain loop**

Edit `app_chat.go` lines 118-151:

Old:
```go
ep := agent.NewEmotionParser()
for result := range ch {
    // ... (lines 120-141 unchanged) ...
    text, emotion, intensity := ep.Feed(result.Token)
    if emotion != "" {
        wailsruntime.EventsEmit(a.ctx, "chat:emotion", map[string]any{
            "emotion":   emotion,
            "intensity": intensity,
        })
    }
    if text != "" {
        wailsruntime.EventsEmit(a.ctx, "chat:token", text)
    }
}
```

New:
```go
bp := agent.NewBehaviorParser()
for result := range ch {
    // ... (lines 120-141 unchanged) ...
    text, emotion, action := bp.Feed(result.Token)
    if emotion != "" {
        wailsruntime.EventsEmit(a.ctx, "chat:behavior", map[string]any{
            "emotion": emotion,
            "action":  action,
        })
    }
    if text != "" {
        wailsruntime.EventsEmit(a.ctx, "chat:token", text)
    }
}
```

Also update the Flush call reference (line 129):
Old: `if tail := ep.Flush(); tail != "" {`
New: `if tail := bp.Flush(); tail != "" {`

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/xutiancheng/code/self/Aiko && go build ./...`
Expected: compile success

- [ ] **Step 4: Run all tests**

Run: `cd /Users/xutiancheng/code/self/Aiko && go test ./internal/agent/... -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add app_chat.go internal/agent/context.go
git commit -m "refactor: switch drain loop from EmotionParser to BehaviorParser, emit chat:behavior"
```

---

### Task 5: Create useBehaviorEvents composable

**Files:**
- Create: `frontend/src/composables/useBehaviorEvents.js`

- [ ] **Step 1: Write useBehaviorEvents.js**

Write: `frontend/src/composables/useBehaviorEvents.js`

```js
import { onUnmounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'

/**
 * useBehaviorEvents subscribes to the chat:behavior Wails event and forwards
 * behavior data to the pet renderer via the provided callback.
 * Automatically unsubscribes when the calling component is unmounted.
 * @param {function({emotion: string, action: string}): void} onBehavior
 */
export function useBehaviorEvents(onBehavior) {
  const off = EventsOn('chat:behavior', (data) => {
    if (data && typeof data.emotion === 'string') {
      onBehavior({ emotion: data.emotion, action: data.action || '' })
    }
  })
  onUnmounted(() => { off?.() })
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/composables/useBehaviorEvents.js
git commit -m "feat(frontend): add useBehaviorEvents composable for chat:behavior events"
```

---

### Task 6: Revert Live2DPet.vue emotion changes

**Files:**
- Modify: `frontend/src/components/Live2DPet.vue`

- [ ] **Step 1: Remove useEmotionEvents import**

Edit line 11 — remove:
```js
import { useEmotionEvents } from '../composables/useEmotionEvents.js'
```

- [ ] **Step 2: Remove EMOTION_EXPRESSION_KEYWORDS and applyEmotionToExpression**

Delete lines 50-78 (from `/**` before EMOTION_EXPRESSION_KEYWORDS through the closing `}` of applyEmotionToExpression).

- [ ] **Step 3: Remove useEmotionEvents subscription call**

Delete lines 303-306:
```js
// Subscribe to LLM emotion events — drives expression independently from pet state.
useEmotionEvents(({ emotion, intensity }) =>
  applyEmotionToExpression({ emotion, intensity }),
)
```

- [ ] **Step 4: Restore hardcoded expressions in watch(petState)**

Edit the watch(petState) block — restore `.expression()` calls:

Old (current lines 313-335):
```js
watch(petState, (state) => {
  if (!live2dModel) return
  switch (state) {
    case 'thinking':
      live2dModel.motion('Idle', undefined, MotionPriority.NORMAL)
      break
    case 'speaking':
      live2dModel.motion('TapBody', undefined, MotionPriority.FORCE)
      break
    case 'listening':
      live2dModel.motion('Idle', undefined, MotionPriority.NORMAL)
      break
    case 'error':
      live2dModel.motion('TapBody', undefined, MotionPriority.FORCE)
      live2dModel.expression(namedExpr('生气'))
      break
    case 'idle':
    default:
      live2dModel.motion('Idle', undefined, MotionPriority.IDLE)
      live2dModel.expression()
      break
  }
})
```

New:
```js
watch(petState, (state) => {
  if (!live2dModel) return
  switch (state) {
    case 'thinking':
      live2dModel.motion('Idle', undefined, MotionPriority.NORMAL)
      live2dModel.expression(namedExpr('星星眼'))
      break
    case 'speaking':
      live2dModel.motion('TapBody', undefined, MotionPriority.FORCE)
      live2dModel.expression(namedExpr('爱心'))
      break
    case 'listening':
      live2dModel.motion('Idle', undefined, MotionPriority.NORMAL)
      live2dModel.expression(namedExpr('星星眼'))
      break
    case 'error':
      live2dModel.motion('TapBody', undefined, MotionPriority.FORCE)
      live2dModel.expression(namedExpr('生气'))
      break
    case 'idle':
    default:
      live2dModel.motion('Idle', undefined, MotionPriority.IDLE)
      live2dModel.expression()
      break
  }
})
```

- [ ] **Step 5: Verify build**

Run: `cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build`
Expected: build success (Live2DPet no longer imports useEmotionEvents)

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/Live2DPet.vue
git commit -m "revert: remove Live2D emotion tag support (model has no expression files)"
```

---

### Task 7: Refactor VRMPet.vue animation system

**Files:**
- Modify: `frontend/src/components/VRMPet.vue`

- [ ] **Step 1: Replace useEmotionEvents import with useBehaviorEvents**

Edit line 34:

Old:
```js
import { useEmotionEvents } from "../composables/useEmotionEvents.js";
```

New:
```js
import { useBehaviorEvents } from "../composables/useBehaviorEvents.js";
```

- [ ] **Step 2: Replace EMOTION_MAP and EMOTION_SPEAKING_ANIMS with new mapping tables**

Delete lines 77-82 and 220-226, add new tables after the `VRM_HEAD_CANVAS_OFFSET` block (~line 92):

```js
// LLM emotion → VRM blendshape name
const EMOTION_MAP = {
  joy: "happy",
  sad: "sad",
  surprised: "surprised",
  angry: "angry",
};

// LLM emotion → sustained animation (applies to ALL pet states, loop)
const EMOTION_ANIMS = {
  joy: "/vrm/celebrate.vrma",
  sad: "/vrm/sad.vrma",
  surprised: "/vrm/surprised_react.vrma",
  angry: "/vrm/angry.vrma",
};

// LLM action → one-shot animation (plays once, then returns to current animation)
const ACTION_ANIMS = {
  wave: "/vrm/wave_big.vrma",
  nod: "/vrm/nod.vrma",
  celebrate: "/vrm/celebrate.vrma",
  surprised_react: "/vrm/surprised_react.vrma",
};
```

- [ ] **Step 3: Remove old EMOTION_SPEAKING_ANIMS**

Delete the old `EMOTION_SPEAKING_ANIMS` block (should have been replaced in step 2).

- [ ] **Step 4: Replace `_speakingEmotion` with `_activeEmotion`**

Edit line 240:

Old:
```js
let _speakingEmotion = null; // last emotion received, used on speaking state entry
```

New:
```js
let _activeEmotion = null; // current sustained emotion (null when neutral/idle-cleared)
```

- [ ] **Step 5: Rewrite setState**

Replace lines 96-117:

Old:
```js
function setState(state) {
  mouthPhase = 0;
  switch (state) {
    case "idle":
      Object.keys(targetEmotionWeights).forEach((k) => {
        targetEmotionWeights[k] = 0;
      });
      break;
    case "listening":
      Object.keys(targetEmotionWeights).forEach((k) => {
        targetEmotionWeights[k] = 0;
      });
      break;
    case "error":
      Object.keys(targetEmotionWeights).forEach((k) => {
        targetEmotionWeights[k] = 0;
      });
      targetEmotionWeights["sad"] = 0.6;
      break;
    // thinking and speaking: keep current emotion, speaking drives mouth anim
  }
}
```

New:
```js
function setState(state) {
  mouthPhase = 0;
  switch (state) {
    case "idle":
      // Clear emotion state — pet returns to neutral waiting.
      _activeEmotion = null;
      Object.keys(targetEmotionWeights).forEach((k) => {
        targetEmotionWeights[k] = 0;
      });
      break;
    case "error":
      // Error always overrides with sad expression.
      _activeEmotion = null;
      Object.keys(targetEmotionWeights).forEach((k) => {
        targetEmotionWeights[k] = 0;
      });
      targetEmotionWeights["sad"] = 0.6;
      break;
    // thinking, speaking, listening: keep active emotion if set.
  }
}
```

- [ ] **Step 6: Rewrite applyEmotion → applyBehavior**

Replace lines 129-141:

Old:
```js
function applyEmotion({ emotion, intensity }) {
  const mapped = EMOTION_MAP[emotion];
  Object.keys(targetEmotionWeights).forEach((k) => {
    targetEmotionWeights[k] = 0;
  });
  if (mapped)
    targetEmotionWeights[mapped] = Math.max(0, Math.min(1, intensity));
  // Store for speaking animation override; clear on low intensity.
  _speakingEmotion =
    intensity >= 0.4 && EMOTION_SPEAKING_ANIMS[emotion] ? emotion : null;
  // If already speaking, switch animation immediately.
  if (petState.value === "speaking") applyStateAnimation("speaking");
}
```

New:
```js
/**
 * applyBehavior handles a chat:behavior event.
 * 1. If action is set → play one-shot animation, then return to sustained/fallback.
 * 2. If emotion ≠ neutral → set blendshape + switch sustained animation.
 * 3. If emotion = neutral → clear blendshape, return to petState fallback.
 */
function applyBehavior({ emotion, action }) {
  // Reset blendshape targets.
  Object.keys(targetEmotionWeights).forEach((k) => {
    targetEmotionWeights[k] = 0;
  });

  // 1. One-shot action — plays once, then auto-returns to sustained/fallback.
  if (action && ACTION_ANIMS[action]) {
    playAnimation(ACTION_ANIMS[action], { loop: false, fadeTime: 0.3 }).then(() => {
      // After action completes, apply sustained animation.
      _applySustainedAnimation(emotion);
    });
    return;
  }

  // 2. No action — apply sustained animation directly.
  _applySustainedAnimation(emotion);
}

/** _applySustainedAnimation sets blendshape and sustained animation for the given emotion. */
function _applySustainedAnimation(emotion) {
  if (emotion === "neutral" || !emotion) {
    _activeEmotion = null;
    applyStateAnimation(petState.value);
    return;
  }

  const mapped = EMOTION_MAP[emotion];
  if (mapped) {
    targetEmotionWeights[mapped] = 0.7;
  }

  _activeEmotion = emotion;
  const animFile = EMOTION_ANIMS[emotion];
  if (animFile) {
    playAnimation(animFile, { loop: true, fadeTime: 0.4 });
  } else {
    applyStateAnimation(petState.value);
  }
}
```

- [ ] **Step 7: Rewrite applyStateAnimation**

Replace lines 303-315:

Old:
```js
async function applyStateAnimation(state) {
  if (state === "speaking") {
    const file =
      EMOTION_SPEAKING_ANIMS[_speakingEmotion] ?? STATE_ANIMS.speaking;
    await playAnimation(file, { fadeTime: 0.4 });
  } else {
    const file = STATE_ANIMS[state];
    if (file) await playAnimation(file, { fadeTime: 0.5 });
  }
  // Reset emotion override when leaving speaking state.
  if (state !== "speaking") _speakingEmotion = null;
}
```

New:
```js
/**
 * applyStateAnimation switches to the fallback animation for the given petState.
 * If an active emotion is set, the emotion animation takes priority over the
 * state fallback (except for error, which always forces its animation).
 */
async function applyStateAnimation(state) {
  // Error always takes priority.
  if (state === "error") {
    await playAnimation(STATE_ANIMS.error, { fadeTime: 0.5 });
    return;
  }

  // Active emotion overrides state fallback.
  if (_activeEmotion && EMOTION_ANIMS[_activeEmotion]) {
    await playAnimation(EMOTION_ANIMS[_activeEmotion], { loop: true, fadeTime: 0.4 });
    return;
  }

  // Speaking with no emotion: hand_talk + mouth blendshape.
  if (state === "speaking") {
    await playAnimation(STATE_ANIMS.speaking, { fadeTime: 0.4 });
    return;
  }

  const file = STATE_ANIMS[state];
  if (file) await playAnimation(file, { fadeTime: 0.5 });
}
```

- [ ] **Step 8: Update defineExpose**

Edit line 152:

Old:
```js
defineExpose({ setState, focusGlobal, applyEmotion, setSize });
```

New:
```js
defineExpose({ setState, focusGlobal, applyBehavior, setSize });
```

- [ ] **Step 9: Replace useEmotionEvents with useBehaviorEvents in lifecycle section**

Replace lines 624-627:

Old:
```js
// Subscribe to emotion events — forwarded to applyEmotion.
useEmotionEvents(({ emotion, intensity }) =>
  applyEmotion({ emotion, intensity }),
);
```

New:
```js
// Subscribe to behavior events — forwarded to applyBehavior.
useBehaviorEvents(({ emotion, action }) =>
  applyBehavior({ emotion, action }),
);
```

- [ ] **Step 10: Verify build**

Run: `cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build`
Expected: build success

- [ ] **Step 11: Commit**

```bash
git add frontend/src/components/VRMPet.vue
git commit -m "refactor(frontend): VRMPet behavior-driven animation system with chat:behavior"
```

---

### Task 8: Final integration verification

**Files:** None (verification only)

- [ ] **Step 1: Full Go compilation + tests**

Run: `cd /Users/xutiancheng/code/self/Aiko && go build ./... && go test ./internal/agent/... -v`
Expected: build success, all tests PASS

- [ ] **Step 2: Full frontend build**

Run: `cd /Users/xutiancheng/code/self/Aiko/frontend && yarn build`
Expected: build success, no warnings about missing imports

- [ ] **Step 3: Full app build**

Run: `cd /Users/xutiancheng/code/self/Aiko && make build`
Expected: builds successfully

- [ ] **Step 4: Final commit**

```bash
git commit -m "chore: final integration verification after behavior system refactor" --allow-empty
```
