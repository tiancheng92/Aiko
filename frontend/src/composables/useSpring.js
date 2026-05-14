/**
 * springAnimate drives a scalar from `from` toward `to` using a damped harmonic
 * oscillator:  a = -stiffness·(x - to) - damping·v
 *
 * Unlike CSS cubic-bezier (polynomial), this is governed by a differential
 * equation whose shape depends on the instantaneous velocity — enabling natural
 * overshoot and oscillation.
 *
 * Damping ratio ζ = damping / (2·√stiffness):
 *   ζ < 1  → underdamped  — bouncy, overshoots target
 *   ζ = 1  → critical     — fastest settle, zero bounce
 *   ζ > 1  → overdamped   — sluggish, no bounce
 *
 * @param {object} opts
 * @param {number} opts.from        Starting value
 * @param {number} opts.to          Target value
 * @param {number} opts.stiffness   Spring constant (higher = faster)
 * @param {number} opts.damping     Damping coefficient
 * @param {number} [opts.restDelta=0.3]    Position threshold to declare "settled"
 * @param {number} [opts.restVelocity=5]   Velocity threshold to declare "settled"
 * @param {function} opts.onUpdate  Called each frame with current value
 * @param {function} [opts.onDone]  Called once settled at target
 * @returns {function} cancel — call to abort the animation
 */
export function springAnimate({
  from, to, stiffness, damping,
  restDelta = 0.3, restVelocity = 5,
  onUpdate, onDone,
}) {
  let pos = from
  let vel = 0
  let last = null
  let raf = null

  function tick(now) {
    if (last === null) last = now
    // Cap dt at 2 frames to survive tab switches / jank spikes.
    const dt = Math.min((now - last) * 0.001, 0.032)
    last = now

    const accel = -stiffness * (pos - to) - damping * vel
    vel += accel * dt
    pos += vel * dt

    onUpdate(pos)

    if (Math.abs(pos - to) < restDelta && Math.abs(vel) < restVelocity) {
      onUpdate(to)
      onDone?.()
      return
    }
    raf = requestAnimationFrame(tick)
  }

  raf = requestAnimationFrame(tick)
  return () => { if (raf !== null) { cancelAnimationFrame(raf); raf = null } }
}
