<script setup>
import { onMounted, onUnmounted, ref } from "vue";

const canvasEl = ref(null);
let rafId = null;
let observer = null;
let resizeTimer = null;
let running = false;

const CHARS =
  "アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン0123456789";
const CHARS_LEN = CHARS.length;
const FONT_SIZE = 12;
const COL_SPACING = 14;
const RESET_THRESHOLD = 0.97;
const DROP_CHANCE = 0.7;
const ALPHA_THRESHOLD = 0.03;

// Time-based constants (per millisecond) for frame-rate independence
// 18 rows/sec fall speed, ~1.1s trail fade (matches original 20fps feel)
const FALL_PX_PER_MS = (18 * FONT_SIZE) / 1000;
const DECAY_PER_MS = -Math.log(0.03) / 1100; // ≈ 0.00324
const EMIT_INTERVAL_MS = 83;

// Pre-computed color LUTs (64 buckets) — avoids CSS string parsing on every draw
// globalAlpha stays at 1.0; alpha is encoded directly in fillStyle strings
const LUT_N = 64;
const LUT_TRAIL = Array.from(
  { length: LUT_N },
  (_, i) => `rgba(0,255,65,${((i / (LUT_N - 1)) * 0.5).toFixed(3)})`,
);
const LUT_HEAD = Array.from(
  { length: LUT_N },
  (_, i) => `rgba(200,255,200,${((i / (LUT_N - 1)) * 0.5).toFixed(3)})`,
);

/** lutColor returns a pre-computed fillStyle string for a trail entry. */
function lutColor(a, isHead) {
  const idx = Math.min(LUT_N - 1, Math.round(a * (LUT_N - 1)));
  return isHead ? LUT_HEAD[idx] : LUT_TRAIL[idx];
}

// Object pool — eliminates per-frame heap allocations and GC pauses
const entryPool = [];
function allocEntry(y, char) {
  const e = entryPool.length > 0 ? entryPool.pop() : {};
  e.y = y;
  e.char = char;
  e.a = 1.0;
  return e;
}
function freeEntry(e) {
  entryPool.push(e);
}

/** initCanvas sizes the canvas to its parent's dimensions. Returns null if size is not yet available. */
function initCanvas(canvas) {
  const parent = canvas.parentElement;
  const w = parent ? parent.clientWidth : canvas.offsetWidth;
  const h = parent ? parent.clientHeight : canvas.offsetHeight;
  if (w === 0 || h === 0) return null;
  canvas.width = w;
  canvas.height = h;
  const cols = Math.floor(w / COL_SPACING);
  const drops = Array.from({ length: cols }, () => ({
    y: Math.random() * -h,
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
  ctx.globalAlpha = 1.0; // fixed at 1; alpha encoded in LUT fillStyle strings
  running = true;
  let lastTime = null;

  function tick(ts) {
    if (!running) return;
    if (lastTime === null) {
      lastTime = ts;
      rafId = requestAnimationFrame(tick);
      return;
    }

    const dt = Math.min(ts - lastTime, 50);
    lastTime = ts;

    ctx.clearRect(0, 0, canvas.width, canvas.height);

    const decayFactor = Math.exp(-DECAY_PER_MS * dt);

    for (let i = 0; i < drops.length; i++) {
      const drop = drops[i];
      const trail = trails[i];

      // Decay + compact in one pass — no splice, no shifting, no GC
      let write = 0;
      for (let j = 0; j < trail.length; j++) {
        trail[j].a *= decayFactor;
        if (trail[j].a >= ALPHA_THRESHOLD) {
          trail[write++] = trail[j];
        } else {
          freeEntry(trail[j]);
        }
      }
      trail.length = write;

      // Emit head character from pool
      drop.emitTimer += dt;
      if (drop.emitTimer >= EMIT_INTERVAL_MS) {
        drop.emitTimer -= EMIT_INTERVAL_MS;
        if (Math.random() < DROP_CHANCE) {
          trail.push(
            allocEntry(drop.y, CHARS[Math.floor(Math.random() * CHARS_LEN)]),
          );
        }
      }

      // Draw trail; last entry is always the head
      const last = trail.length - 1;
      for (let j = 0; j < trail.length; j++) {
        const t = trail[j];
        ctx.fillStyle = lutColor(t.a, j === last);
        ctx.fillText(t.char, i * COL_SPACING, t.y);
      }

      // Advance drop; reset when it exits the bottom
      drop.y += FALL_PX_PER_MS * dt;
      if (drop.y > canvas.height && Math.random() > RESET_THRESHOLD) {
        for (const e of trail) freeEntry(e);
        trail.length = 0;
        drop.y = -FONT_SIZE;
        drop.emitTimer = 0;
      }
    }
  }

  function loop(ts) {
    tick(ts);
    if (running) rafId = requestAnimationFrame(loop);
  }
  rafId = requestAnimationFrame(loop);
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
