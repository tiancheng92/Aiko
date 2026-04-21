<script setup>
import { ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import * as PIXI from 'pixi.js'
import { Live2DModel, MotionPriority } from 'pixi-live2d-display/cubism4'
import { GetBallPosition, SaveBallPosition, GetScreenSize } from '../../wailsjs/go/main/App'
import { Quit } from '../../wailsjs/runtime/runtime'
import { usePetState } from '../composables/usePetState.js'
import { useModelPath } from '../composables/useModelPath.js'
import ContextMenu from './ContextMenu.vue'

const emit = defineEmits(['click', 'position', 'ball-size', 'open-settings'])

const pos = ref(null)
const { petState } = usePetState()
const { modelPath, loadModels } = useModelPath()
const petMenuRef = ref(null)
const petMenuItems = [
  { icon: '⚙️', label: '打开设置', action: () => emit('open-settings') },
  { divider: true },
  { icon: '❌', label: '退出程序', action: () => Quit() },
]

/** onContextMenu shows the pet right-click menu. */
function onContextMenu(e) {
  e.preventDefault()
  petMenuRef.value?.show(e.clientX, e.clientY)
}
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

/** attachModel loads a Live2D model from path, scales it, and wires up interactions. */
async function attachModel(path) {
  const newModel = await Live2DModel.from(path, { autoInteract: false })
  if (!mounted || !pixiApp) {
    newModel.destroy()
    return
  }
  // Remove old model if present.
  if (live2dModel) {
    live2dModel.off('hit')
    pixiApp.stage.removeChild(live2dModel)
    live2dModel.destroy()
    live2dModel = null
  }
  live2dModel = newModel
  pixiApp.stage.addChild(live2dModel)
  const scale = petSize.value / live2dModel.internalModel.originalHeight
  live2dModel.scale.set(scale)
  live2dModel.anchor.set(0.5, 0.5)
  live2dModel.position.set(petSize.value / 2, petSize.value / 2)
  live2dModel.on('hit', (hitAreas) => {
    if (hitAreas.includes('Body')) {
      live2dModel.motion('TapBody', undefined, MotionPriority.NORMAL)
    }
  })
  live2dModel.motion('Idle', undefined, MotionPriority.IDLE)
}

/** initPixi creates the PixiJS application and loads the initial Live2D model. */
async function initPixi() {
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
  await attachModel(modelPath.value)
}

onMounted(async () => {
  // Kick off model list fetch in background; composable handles errors.
  loadModels()

  // Phase 1: load position — isolated so PixiJS errors can't reset a successfully loaded position.
  try {
    await waitForRuntime()
    // Fallback to window dimensions so sw/sh are never 0 if the Go call fails.
    sw.value = window.innerWidth
    sh.value = window.innerHeight
    try {
      const [screenW, screenH] = await GetScreenSize()
      if (screenW > 0 && screenH > 0) {
        sw.value = screenW
        sh.value = screenH
      }
    } catch (e) {
      console.warn('GetScreenSize failed, using window dimensions', e)
    }
    petSize.value = Math.min(320, Math.max(180, Math.round(sh.value * 0.20)))
    emit('ball-size', petSize.value)

    const [bx, by] = await GetBallPosition(sw.value, sh.value)
    pos.value = (bx >= 0 && by >= 0)
      ? { x: bx, y: by }
      : { x: sw.value - petSize.value - 40, y: sh.value - petSize.value - 40 }
  } catch (err) {
    console.error('Live2DPet position init failed:', err)
    if (!sw.value) sw.value = window.innerWidth
    if (!sh.value) sh.value = window.innerHeight
    const ps = petSize.value
    if (!pos.value) pos.value = { x: window.innerWidth - ps - 40, y: window.innerHeight - ps - 40 }
  }

  // Phase 2: initialize PixiJS — separate try/catch so errors here don't affect saved position.
  await nextTick()
  try {
    await initPixi()
  } catch (err) {
    console.error('Live2DPet PixiJS init failed:', err)
  }
})

/** Watch modelPath and hot-reload the Live2D model when it changes. */
watch(modelPath, async (path) => {
  if (!pixiApp || !mounted) return
  try {
    await attachModel(path)
  } catch (err) {
    console.error('Live2DPet model reload failed:', err)
  }
})

/**
 * watchPetState maps pet states to Live2D motions and expressions.
 * Only applied after the model is loaded (live2dModel is non-null).
 */
watch(petState, (state) => {
  if (!live2dModel) return
  switch (state) {
    case 'thinking':
      live2dModel.motion('Idle', undefined, MotionPriority.NORMAL)
      live2dModel.expression('f01')
      break
    case 'speaking':
      live2dModel.motion('TapBody', undefined, MotionPriority.FORCE)
      live2dModel.expression('f02')
      break
    case 'listening':
      live2dModel.motion('Idle', undefined, MotionPriority.NORMAL)
      live2dModel.expression('f03')
      break
    case 'error':
      live2dModel.motion('TapBody', undefined, MotionPriority.FORCE)
      live2dModel.expression('f04')
      break
    case 'idle':
    default:
      live2dModel.motion('Idle', undefined, MotionPriority.IDLE)
      live2dModel.expression()
      break
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

/** onMouseDown starts drag tracking on left mouse button press. */
function onMouseDown(e) {
  if (e.button !== 0) return
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
    @contextmenu="onContextMenu"
  >
    <canvas ref="canvasRef" class="pet-canvas" />
    <ContextMenu ref="petMenuRef" :items="petMenuItems" />
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
