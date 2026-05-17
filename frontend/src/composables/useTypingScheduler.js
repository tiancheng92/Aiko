import { onScopeDispose } from 'vue'

/**
 * useTypingScheduler queues chat tokens and drains them with variable timing
 * to create a natural typing rhythm: punctuation pauses + subtle speed jitter.
 *
 * Tokens arriving within a single animation frame are batched and applied
 * together (one DOM mutation per frame) to avoid macrotask storms during
 * high-throughput streaming. Punctuation still introduces deliberate inter-
 * frame pauses to preserve the natural rhythm.
 *
 * The returned API is {enqueue, flush, clear}; the composable auto-clears the
 * queue and cancels any pending rAF/setTimeout when its owning scope is disposed
 * (onUnmounted equivalent), so tokens streamed in-flight cannot apply to a
 * destroyed component.
 */
export function useTypingScheduler(applyToken) {
  const PUNCT = new Set(['。', '！', '？', '\n', '，', '、', '…', '；', '!', '?', ';'])
  const PUNCT_MIN_MS = 120
  const PUNCT_MAX_MS = 200

  const queue = []
  let rafId = null
  let pauseUntil = 0   // performance.now() timestamp; 0 = no pause
  let disposed = false

  /** scheduleFrame requests the next animation-frame drain pass if not already pending. */
  function scheduleFrame() {
    if (rafId !== null || disposed) return
    rafId = requestAnimationFrame(drainFrame)
  }

  /**
   * drainFrame runs each animation frame: applies all tokens queued since the
   * last frame in a single batch. If the last token was a punctuation character,
   * it schedules a deliberate pause before the next frame instead of continuing
   * immediately.
   */
  function drainFrame() {
    rafId = null
    if (disposed || queue.length === 0) return

    const now = performance.now()
    if (now < pauseUntil) {
      // Still in a punctuation pause — reschedule after remaining delay.
      const remaining = pauseUntil - now
      rafId = setTimeout(() => { rafId = null; scheduleFrame() }, remaining)
      return
    }

    // Apply every token that has arrived since the last frame.
    let lastToken = null
    while (queue.length > 0) {
      lastToken = queue.shift()
      applyToken(lastToken)
    }

    // If the batch ended on a punctuation character, pause before next frame.
    if (lastToken !== null) {
      const last = lastToken[lastToken.length - 1]
      if (PUNCT.has(last)) {
        pauseUntil = performance.now() + PUNCT_MIN_MS + Math.random() * (PUNCT_MAX_MS - PUNCT_MIN_MS)
        rafId = setTimeout(() => { rafId = null; scheduleFrame() }, pauseUntil - performance.now())
        return
      }
    }

    if (queue.length > 0) scheduleFrame()
  }

  /** enqueue adds a token to the queue and kicks off a drain frame if idle. */
  function enqueue(token) {
    if (disposed) return
    queue.push(token)
    scheduleFrame()
  }

  /** flush drains all remaining queued tokens immediately (no delay). */
  function flush() {
    if (rafId !== null) {
      if (typeof rafId === 'number') {
        // Could be either a rAF id or a setTimeout id; cancel both to be safe.
        cancelAnimationFrame(rafId)
        clearTimeout(rafId)
      }
      rafId = null
    }
    pauseUntil = 0
    while (queue.length > 0) applyToken(queue.shift())
  }

  /** clear discards all queued tokens without applying them. */
  function clear() {
    if (rafId !== null) {
      cancelAnimationFrame(rafId)
      clearTimeout(rafId)
      rafId = null
    }
    pauseUntil = 0
    queue.length = 0
  }

  onScopeDispose(() => {
    disposed = true
    clear()
  })

  return { enqueue, flush, clear }
}
