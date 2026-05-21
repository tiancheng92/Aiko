<script setup>
import { onMounted, onUnmounted, ref } from "vue";

const canvasEl = ref(null);
let rafId = null;
let observer = null;
let resizeTimer = null;
let running = false;

const CHARS =
  "アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン";
const CHARS_LEN = CHARS.length;
const FONT_SIZE = 14;
const COL_SPACING = 24;
const RESET_THRESHOLD = 0.97;
const DROP_CHANCE = 0.6;
const HEAD_COLOR = "rgba(200, 255, 200, 1)";
const TRAIL_COLOR = "rgba(0, 255, 65, 0.5)";
const ALPHA_THRESHOLD = 0.03;

// Time-based constants (per millisecond) for frame-rate independence
// 18 rows/sec fall speed, ~1.1s trail fade (matches original 20fps feel)
const FALL_PX_PER_MS    = (18 * FONT_SIZE) / 1000;
const DECAY_PER_MS      = -Math.log(0.03) / 1100;   // ≈ 0.00324
const EMIT_INTERVAL_MS  = 83;                         // ~12 chars/sec per column

/** initCanvas sizes the canvas to its parent's dimensions. Returns null if size is not yet available. */
function initCanvas(canvas) {
  const parent = canvas.parentElement;
  const w = parent ? parent.clientWidth  : canvas.offsetWidth;
  const h = parent ? parent.clientHeight : canvas.offsetHeight;
  if (w === 0 || h === 0) return null;
  canvas.width  = w;
  canvas.height = h;
  const cols = Math.floor(w / COL_SPACING);
  const drops = Array.from({ length: cols }, () => ({
    y:         Math.random() * -h,
    emitTimer: Math.random() * EMIT_INTERVAL_MS,
  }));
  const trails = drops.map(() => []);
  return { drops, trails };
}

/** startAnimation runs a rAF loop on the canvas. Call stopAnimation() to cancel. */
function startAnimation(canvas) {
  const ctx = canvas.getContext("2d");
  const state = initCanvas(canvas);
  if (!state) return;
  const { drops, trails } = state;

  ctx.font = FONT_SIZE + "px monospace";
  running = true;
  let lastTime = null;

  function frame(ts) {
    if (!running) return;
    if (lastTime === null) { lastTime = ts; rafId = requestAnimationFrame(frame); return; }

    const dt = Math.min(ts - lastTime, 50); // cap delta to handle tab-hidden re-entry
    lastTime = ts;

    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const decayFactor = Math.exp(-DECAY_PER_MS * dt);

    for (let i = 0; i < drops.length; i++) {
      const drop  = drops[i];
      const trail = trails[i];

      // Decay existing trail entries
      for (let j = trail.length - 1; j >= 0; j--) {
        trail[j].a *= decayFactor;
        if (trail[j].a < ALPHA_THRESHOLD) trail.splice(j, 1);
      }

      // Emit new head character on interval
      drop.emitTimer += dt;
      if (drop.emitTimer >= EMIT_INTERVAL_MS) {
        drop.emitTimer -= EMIT_INTERVAL_MS;
        if (Math.random() < DROP_CHANCE) {
          trail.push({ y: drop.y, char: CHARS[Math.floor(Math.random() * CHARS_LEN)], a: 1.0 });
        }
      }

      // Draw trail; last entry is always the head
      for (let j = 0; j < trail.length; j++) {
        const t = trail[j];
        ctx.globalAlpha = t.a;
        ctx.fillStyle   = j === trail.length - 1 ? HEAD_COLOR : TRAIL_COLOR;
        ctx.fillText(t.char, i * COL_SPACING, t.y);
      }

      // Advance drop; reset when it exits the bottom
      drop.y += FALL_PX_PER_MS * dt;
      if (drop.y > canvas.height && Math.random() > RESET_THRESHOLD) {
        drop.y = -FONT_SIZE;
        drop.emitTimer = 0;
        trails[i] = [];
      }
    }

    ctx.globalAlpha = 1.0;
    rafId = requestAnimationFrame(frame);
  }

  rafId = requestAnimationFrame(frame);
}

/** stopAnimation cancels the running rAF loop. */
function stopAnimation() {
  running = false;
  cancelAnimationFrame(rafId);
  rafId = null;
}

onMounted(() => {
  const canvas = canvasEl.value;
  const parent = canvas.parentElement;

  requestAnimationFrame(() => {
    startAnimation(canvas);

    observer = new ResizeObserver(() => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => {
        stopAnimation();
        startAnimation(canvas);
      }, 16);
    });
    observer.observe(parent ?? canvas);
  });
});

onUnmounted(() => {
  stopAnimation();
  clearTimeout(resizeTimer);
  observer?.disconnect();
});
</script>

<template>
  <canvas ref="canvasEl" class="code-rain" />
</template>

<style scoped>
.code-rain {
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  display: block;
}
</style>
