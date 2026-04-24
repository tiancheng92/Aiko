/** useSounds provides cute synthesized sound effects for chat interactions.
 *  Sounds are generated via Web Audio API — no external files required.
 *  AudioContext is lazy-initialized on first play() call (requires prior user gesture). */
export function useSounds() {
  let ctx = null

  /** ensureCtx lazily creates the AudioContext on first use. */
  function ensureCtx() {
    if (!ctx) ctx = new (window.AudioContext || window.webkitAudioContext)()
    if (ctx.state === 'suspended') ctx.resume()
    return ctx
  }

  /** playTone synthesizes a short tone with the given parameters.
   *  @param {number} freq - frequency in Hz
   *  @param {number} duration - duration in seconds
   *  @param {string} type - oscillator type: 'sine' | 'triangle' | 'square'
   *  @param {number} volume - gain 0–1
   *  @param {number} [freqEnd] - optional end frequency for a glide effect */
  function playTone(freq, duration, type, volume, freqEnd) {
    try {
      const ac = ensureCtx()
      const osc = ac.createOscillator()
      const gain = ac.createGain()
      osc.connect(gain)
      gain.connect(ac.destination)
      osc.type = type
      osc.frequency.setValueAtTime(freq, ac.currentTime)
      if (freqEnd !== undefined) {
        osc.frequency.linearRampToValueAtTime(freqEnd, ac.currentTime + duration)
      }
      gain.gain.setValueAtTime(volume, ac.currentTime)
      gain.gain.exponentialRampToValueAtTime(0.001, ac.currentTime + duration)
      osc.start(ac.currentTime)
      osc.stop(ac.currentTime + duration)
    } catch (e) {
      // Silently ignore audio errors (e.g. tab not focused, AudioContext suspended)
      console.debug('useSounds playTone error:', e)
    }
  }

  /** playSend plays a short upward "tik" for message send. */
  function playSend() {
    playTone(880, 0.08, 'sine', 0.15, 1200)
  }

  /** playReceive plays a soft descending "ding" for first AI token. */
  function playReceive() {
    playTone(660, 0.15, 'triangle', 0.12, 520)
  }

  /** playError plays a gentle low "bump" for errors. */
  function playError() {
    playTone(220, 0.25, 'triangle', 0.1, 180)
  }

  /** playStop plays a short downward "tuk" for generation stop. */
  function playStop() {
    playTone(440, 0.1, 'sine', 0.12, 280)
  }

  return { playSend, playReceive, playError, playStop }
}
