<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import * as PIXI from 'pixi.js'
import { Live2DModel, MotionPriority } from 'pixi-live2d-display/cubism4'
import { GetBallPosition, SaveBallPosition, GetScreenSize } from '../../wailsjs/go/main/App'

// Required so pixi-live2d-display can access PIXI.Ticker
window.PIXI = PIXI

const MODEL_PATH = '/live2d/hiyori/Hiyori.model3.json'

const emit = defineEmits(['click', 'position', 'ball-size'])

const pos = ref(null)
const canvasRef = ref(null)
const PET_SIZE = ref(160)
const sw = ref(0)
const sh = ref(0)

let pixiApp = null
let live2dModel = null
let dragStart = null
let isDragging = false

watch(pos, (p) => { if (p) emit('position', { ...p }) })

/** waitForRuntime polls until the Wails Go bridge is available. */
async function waitForRuntime() {
  while (!window.go?.main?.App) {
    await new Promise(r => setTimeout(r, 20))
  }
}

onMounted(async () => {
  try {
    await waitForRuntime()
    const [screenW, screenH] = await GetScreenSize()
    sw.value = screenW
    sh.value = screenH
    PET_SIZE.value = Math.min(200, Math.max(120, Math.round(screenH * 0.14)))
    emit('ball-size', PET_SIZE.value)

    const [bx, by] = await GetBallPosition(screenW, screenH)
    pos.value = (bx >= 0 && by >= 0)
      ? { x: bx, y: by }
      : { x: screenW - PET_SIZE.value - 40, y: screenH - PET_SIZE.value - 40 }

    // Wait for the canvas element to be rendered after pos is set
    await new Promise(r => setTimeout(r, 0))

    // Initialize PixiJS app
    pixiApp = new PIXI.Application({
      view: canvasRef.value,
      width: PET_SIZE.value,
      height: PET_SIZE.value,
      backgroundAlpha: 0,
      antialias: true,
      autoDensity: true,
      resolution: window.devicePixelRatio || 1,
    })

    // Load Live2D model
    live2dModel = await Live2DModel.from(MODEL_PATH, { autoInteract: false })
    pixiApp.stage.addChild(live2dModel)

    // Scale to fit canvas — model is tall (2300×4096), use height as limiting dimension
    const scale = PET_SIZE.value / live2dModel.internalModel.originalHeight
    live2dModel.scale.set(scale)
    live2dModel.anchor.set(0.5, 0.5)
    live2dModel.position.set(PET_SIZE.value / 2, PET_SIZE.value / 2)

    // Start idle loop
    live2dModel.motion('Idle', undefined, MotionPriority.IDLE)
  } catch (err) {
    console.error('Live2DPet init:', err)
    const ps = PET_SIZE.value
    pos.value = { x: window.innerWidth - ps - 40, y: window.innerHeight - ps - 40 }
  }
})

onUnmounted(() => {
  if (pixiApp) {
    pixiApp.destroy(false, { children: true, texture: true, baseTexture: true })
    pixiApp = null
    live2dModel = null
  }
})

/** onCanvasMouseMove updates Live2D eye/body tracking using canvas-relative coordinates. */
function onCanvasMouseMove(e) {
  if (!live2dModel || isDragging) return
  const rect = canvasRef.value.getBoundingClientRect()
  live2dModel.focus(e.clientX - rect.left, e.clientY - rect.top)
}

/** onCanvasClick triggers a tap interaction on the Live2D model and emits click. */
function onCanvasClick(e) {
  if (isDragging || !live2dModel) return
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
    console.error('Failed to save ball position:', err)
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
    :style="{ left: pos.x + 'px', top: pos.y + 'px', width: PET_SIZE + 'px', height: PET_SIZE + 'px' }"
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
