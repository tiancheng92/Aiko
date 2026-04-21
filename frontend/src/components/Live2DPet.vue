<script setup>
import { ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import * as PIXI from 'pixi.js'
import { Live2DModel, MotionPriority } from 'pixi-live2d-display/cubism4'
import { GetBallPosition, SaveBallPosition, GetScreenSize } from '../../wailsjs/go/main/App'

const MODEL_PATH = '/live2d/hiyori/Hiyori.model3.json'

const emit = defineEmits(['click', 'position', 'ball-size'])

const pos = ref(null)
const canvasRef = ref(null)
const petSize = ref(160)
const sw = ref(0)
const sh = ref(0)

let pixiApp = null
let live2dModel = null
let dragStart = null
let isDragging = false
let mounted = true

watch(pos, (p) => { if (p) emit('position', { ...p }) })

/** waitForRuntime polls until the Wails Go bridge is available. */
async function waitForRuntime() {
  while (!window.go?.main?.App) {
    await new Promise(r => setTimeout(r, 20))
  }
}

/** initPixi creates the PixiJS application and loads the Live2D model. */
async function initPixi() {
  // Use registerTicker instead of window.PIXI global to avoid module-level side effects.
  Live2DModel.registerTicker(PIXI.Ticker)

  pixiApp = new PIXI.Application({
    view: canvasRef.value,
    width: petSize.value,
    height: petSize.value,
    backgroundAlpha: 0,
    antialias: true,
    autoDensity: true,
    resolution: window.devicePixelRatio || 1,
  })

  live2dModel = await Live2DModel.from(MODEL_PATH, { autoInteract: false })

  // Guard against component unmounting while model was loading.
  if (!mounted) {
    live2dModel.destroy()
    return
  }

  pixiApp.stage.addChild(live2dModel)

  // Scale to fit canvas — Hiyori is a tall portrait model (~2300×4096); limit by height.
  const scale = petSize.value / live2dModel.internalModel.originalHeight
  live2dModel.scale.set(scale)
  live2dModel.anchor.set(0.5, 0.5)
  live2dModel.position.set(petSize.value / 2, petSize.value / 2)

  // Play TapBody motion when the model's body hit area is tapped.
  live2dModel.on('hit', (hitAreas) => {
    if (hitAreas.includes('Body')) {
      live2dModel.motion('TapBody', undefined, MotionPriority.NORMAL)
    }
  })

  // Start idle animation loop.
  live2dModel.motion('Idle', undefined, MotionPriority.IDLE)
}

onMounted(async () => {
  try {
    await waitForRuntime()
    const [screenW, screenH] = await GetScreenSize()
    sw.value = screenW
    sh.value = screenH
    petSize.value = Math.min(200, Math.max(120, Math.round(screenH * 0.14)))
    emit('ball-size', petSize.value)

    const [bx, by] = await GetBallPosition(screenW, screenH)
    pos.value = (bx >= 0 && by >= 0)
      ? { x: bx, y: by }
      : { x: screenW - petSize.value - 40, y: screenH - petSize.value - 40 }

    // Wait for Vue to render the canvas element before passing it to PixiJS.
    await nextTick()
    await initPixi()
  } catch (err) {
    console.error('Live2DPet init:', err)
    const ps = petSize.value
    pos.value = { x: window.innerWidth - ps - 40, y: window.innerHeight - ps - 40 }
  }
})

onUnmounted(() => {
  mounted = false
  if (live2dModel) {
    live2dModel.off('hit')
    live2dModel = null
  }
  if (pixiApp) {
    pixiApp.destroy(true, { children: true, texture: true, baseTexture: true })
    pixiApp = null
  }
})

/** onCanvasMouseMove updates Live2D eye/body tracking using canvas-relative coordinates. */
function onCanvasMouseMove(e) {
  if (!live2dModel || !canvasRef.value || isDragging) return
  const rect = canvasRef.value.getBoundingClientRect()
  live2dModel.focus(e.clientX - rect.left, e.clientY - rect.top)
}

/** onCanvasClick triggers a tap interaction on the Live2D model and emits click. */
function onCanvasClick(e) {
  if (isDragging || !live2dModel || !canvasRef.value) return
  const rect = canvasRef.value.getBoundingClientRect()
  live2dModel.tap(e.clientX - rect.left, e.clientY - rect.top)
  emit('click')
}

/** onMouseDown starts drag tracking on mouse button press. */
function onMouseDown(e) {
  dragStart = { x: e.clientX - pos.value.x, y: e.clientY - pos.value.y, startX: e.clientX, startY: e.clientY }
  isDragging = false
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}

/** onMouseMove updates the pet position during drag. */
function onMouseMove(e) {
  if (!dragStart || !pos.value) return
  const dx = e.clientX - dragStart.startX
  const dy = e.clientY - dragStart.startY
  if (!isDragging && Math.sqrt(dx * dx + dy * dy) < 5) return
  isDragging = true
  pos.value = { x: e.clientX - dragStart.x, y: e.clientY - dragStart.y }
}

/** onMouseUp finalizes drag or fires click event, then persists position. */
async function onMouseUp(e) {
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  try {
    if (!isDragging) {
      onCanvasClick(e)
    } else {
      await SaveBallPosition(Math.round(pos.value.x), Math.round(pos.value.y), sw.value, sh.value)
    }
  } catch (err) {
    console.error('Failed to save pet position:', err)
  } finally {
    dragStart = null
    isDragging = false
  }
}
</script>

<template>
  <div
    v-if="pos"
    class="live2d-pet"
    :style="{ left: pos.x + 'px', top: pos.y + 'px', width: petSize + 'px', height: petSize + 'px' }"
    @mousedown="onMouseDown"
    @mousemove="onCanvasMouseMove"
  >
    <canvas ref="canvasRef" class="pet-canvas" />
  </div>
</template>

<style scoped>
.live2d-pet {
  position: fixed;
  z-index: 9999;
  cursor: pointer;
  user-select: none;
}
.pet-canvas {
  width: 100%;
  height: 100%;
  display: block;
}
</style>
