/** useTypingScheduler queues chat tokens and drains them with variable timing to create
 *  a natural typing rhythm: punctuation pauses + subtle speed jitter. */
export function useTypingScheduler(applyToken) {
  const PUNCT = new Set(['。', '！', '？', '\n', '，', '、', '…', '；', '!', '?', ';'])
  const BASE_DELAY_MS = 16
  const JITTER_MS = 8
  const PUNCT_MIN_MS = 120
  const PUNCT_MAX_MS = 200

  const queue = []
  let draining = false

  /** computeDelay returns the ms to wait before rendering this token. */
  function computeDelay(token) {
    const last = token[token.length - 1]
    if (PUNCT.has(last)) {
      return PUNCT_MIN_MS + Math.random() * (PUNCT_MAX_MS - PUNCT_MIN_MS)
    }
    return BASE_DELAY_MS + (Math.random() * 2 - 1) * JITTER_MS
  }

  /** drain processes the next token from the queue, then schedules itself again. */
  function drain() {
    if (queue.length === 0) {
      draining = false
      return
    }
    const token = queue.shift()
    applyToken(token)
    setTimeout(drain, computeDelay(token))
  }

  /** enqueue adds a token to the queue and starts draining if not already running. */
  function enqueue(token) {
    queue.push(token)
    if (!draining) {
      draining = true
      drain()
    }
  }

  /** flush drains all remaining queued tokens immediately (no delay). */
  function flush() {
    while (queue.length > 0) {
      applyToken(queue.shift())
    }
    draining = false
  }

  /** clear discards all queued tokens without applying them. */
  function clear() {
    queue.length = 0
    draining = false
  }

  return { enqueue, flush, clear }
}
