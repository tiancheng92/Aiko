/**
 * Returns a throttled version of fn that fires at most once every `wait` ms.
 * The trailing call is always delivered so the final value is never dropped.
 * @param {Function} fn
 * @param {number} wait - milliseconds
 * @returns {Function}
 */
export function throttle(fn, wait) {
  let lastTime = 0
  let timer = null
  return function (...args) {
    const now = Date.now()
    const remaining = wait - (now - lastTime)
    if (remaining <= 0) {
      if (timer !== null) {
        clearTimeout(timer)
        timer = null
      }
      lastTime = now
      fn.apply(this, args)
    } else {
      clearTimeout(timer)
      timer = setTimeout(() => {
        lastTime = Date.now()
        timer = null
        fn.apply(this, args)
      }, remaining)
    }
  }
}

/**
 * Returns a debounced version of fn that delays invocation until `wait` ms
 * after the last call.
 * @param {Function} fn
 * @param {number} wait - milliseconds
 * @returns {Function}
 */
export function debounce(fn, wait) {
  let timer = null
  return function (...args) {
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = null
      fn.apply(this, args)
    }, wait)
  }
}
