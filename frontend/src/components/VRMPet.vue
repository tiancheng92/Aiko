<script setup>
import { ref, watch, computed, onMounted, onUnmounted, nextTick } from 'vue'
import * as THREE from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { VRMLoaderPlugin, VRMUtils } from '@pixiv/three-vrm'
import { VRMAnimationLoaderPlugin, createVRMAnimationClip } from '@pixiv/three-vrm-animation'
import {
  GetBallPosition, SaveBallPosition, GetScreenSize, GetConfig,
  SaveConfig, GetMousePosition, GetPetSize, ListVRMModels, ImportVRMFile
} from '../../wailsjs/go/main/App'
import { Quit, EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { usePetState } from '../composables/usePetState.js'
import { useVRMModel } from '../composables/useVRMModel.js'
import { useEmotionEvents } from '../composables/useEmotionEvents.js'
import ContextMenu from './ContextMenu.vue'
import { ICON_SHIRT, ICON_SETTING, ICON_POWER } from '../utils/icons'

const emit = defineEmits(['click', 'position', 'ball-size', 'open-settings'])
const props = defineProps({
  activeScreen: { type: Object, default: () => ({ width: 0, height: 0 }) },
})

const canvasRef = ref(null)
const pos = ref(null)
const petSize = ref(200)
const sw = ref(0)
const sh = ref(0)
const petMenuRef = ref(null)
const { petState } = usePetState()
const { currentVRMModel, availableVRMModels, vrmModelURL, loadVRMModels } = useVRMModel()

let scene, camera, renderer, vrm, clock, rafId, idleMixer
let mounted = true
let mouseTrackTimer = null
let isDragging = false
let dragStart = null
let mouthPhase = 0
let blinkTimer = null
const MOUSE_POLL_MS = 50

// Head IK targets (-1 to 1 normalized)
let targetHeadX = 0
let targetHeadY = 0

// Emotion blendshape targets and current values
const targetEmotionWeights = {}
const currentEmotionWeights = {}

const EMOTION_MAP = { joy: 'happy', sad: 'sad', surprised: 'surprised', angry: 'angry' }

watch(pos, (p) => { if (p) emit('position', { ...p }) })

// ── Imperative API (exposed to App.vue) ─────────────────────────────────────

/** setState maps pet states to VRM animations and expressions. */
function setState(state) {
  mouthPhase = 0
  switch (state) {
    case 'idle':
      Object.keys(targetEmotionWeights).forEach(k => { targetEmotionWeights[k] = 0 })
      break
    case 'listening':
      Object.keys(targetEmotionWeights).forEach(k => { targetEmotionWeights[k] = 0 })
      break
    case 'error':
      Object.keys(targetEmotionWeights).forEach(k => { targetEmotionWeights[k] = 0 })
      targetEmotionWeights['sad'] = 0.6
      break
    // thinking and speaking: keep current emotion, speaking drives mouth anim
  }
}

/** focusGlobal transforms global screen coordinates into head IK targets. */
function focusGlobal(mx, my) {
  if (!canvasRef.value) return
  const rect = canvasRef.value.getBoundingClientRect()
  const cx = rect.left + rect.width / 2
  const cy = rect.top + rect.height / 2
  targetHeadX = Math.max(-1, Math.min(1, (mx - cx) / (rect.width * 1.5)))
  targetHeadY = Math.max(-0.5, Math.min(0.5, (my - cy) / (rect.height * 1.5)))
}

/** applyEmotion sets blendshape targets and records emotion for speaking animation. */
function applyEmotion({ emotion, intensity }) {
  const mapped = EMOTION_MAP[emotion]
  Object.keys(targetEmotionWeights).forEach(k => { targetEmotionWeights[k] = 0 })
  if (mapped) targetEmotionWeights[mapped] = Math.max(0, Math.min(1, intensity))
  // Store for speaking animation override; clear on low intensity.
  _speakingEmotion = (intensity >= 0.4 && EMOTION_SPEAKING_ANIMS[emotion]) ? emotion : null
  // If already speaking, switch animation immediately.
  if (petState.value === 'speaking') applyStateAnimation('speaking')
}

/** setSize resizes the renderer to n×n pixels. */
function setSize(n) {
  if (!renderer || n <= 0) return
  petSize.value = n
  renderer.setSize(n, n)
  emit('ball-size', n)
}

defineExpose({ setState, focusGlobal, applyEmotion, setSize })

// ── Render Loop Sub-Systems ──────────────────────────────────────────────────

/** updateHeadIK smoothly rotates head and neck bones toward the cursor. */
function updateHeadIK(dt) {
  if (!vrm) return
  const head = vrm.humanoid?.getNormalizedBoneNode('head')
  const neck = vrm.humanoid?.getNormalizedBoneNode('neck')
  const speed = dt * 5
  if (head) {
    head.rotation.y += (targetHeadX * (Math.PI / 5) - head.rotation.y) * speed  // ±36°
    head.rotation.x += (targetHeadY * (Math.PI / 8) - head.rotation.x) * speed  // ±11°
  }
  if (neck) {
    neck.rotation.y += (targetHeadX * (Math.PI / 10) - neck.rotation.y) * speed // ±18°
    neck.rotation.x += (targetHeadY * (Math.PI / 16) - neck.rotation.x) * speed // ±5.6°
  }
}

/** updateMouthAnim drives the aa blendshape in a sine wave while speaking. */
function updateMouthAnim(dt) {
  if (!vrm?.expressionManager) return
  if (petState.value === 'speaking') {
    mouthPhase += dt * 8
    const v = (Math.sin(mouthPhase) + 1) * 0.3
    try { vrm.expressionManager.setValue('aa', v) } catch (_) {}
  } else {
    const cur = vrm.expressionManager.getValue('aa') ?? 0
    if (cur > 0.001) {
      try { vrm.expressionManager.setValue('aa', Math.max(0, cur - dt * 2)) } catch (_) {}
    }
  }
}

/** updateEmotionBlend lerps emotion blendshape weights toward targets each frame. */
function updateEmotionBlend(dt) {
  if (!vrm?.expressionManager) return
  const speed = dt * 4
  const allKeys = new Set([...Object.keys(targetEmotionWeights), ...Object.keys(currentEmotionWeights)])
  for (const name of allKeys) {
    const target = targetEmotionWeights[name] ?? 0
    const cur = currentEmotionWeights[name] ?? 0
    const next = cur + (target - cur) * speed
    currentEmotionWeights[name] = next
    try { vrm.expressionManager.setValue(name, next < 0.001 ? 0 : next) } catch (_) {}
  }
}

// petState → default animation
const STATE_ANIMS = {
  idle:      '/vrm/waiting.vrma',
  listening: '/vrm/curious.vrma',
  thinking:  '/vrm/thinking.vrma',
  speaking:  '/vrm/hand_talk.vrma',
  error:     '/vrm/embarrassed.vrma',
}

// LLM emotion → speaking animation override (fired via chat:emotion)
const EMOTION_SPEAKING_ANIMS = {
  joy:       '/vrm/celebrate.vrma',
  sad:       '/vrm/sad.vrma',
  angry:     '/vrm/angry.vrma',
  surprised: '/vrm/surprised_react.vrma',
}

// Idle variety pool — randomly shown every 25–50s then returns to waiting
const IDLE_VARIETY_POOL = ['/vrm/relaxed.vrma', '/vrm/sleepy.vrma', '/vrm/idle_loop.vrma']

// Occasional one-shot gestures during idle (15–40s interval)
const IDLE_GESTURES = ['/vrm/nod.vrma', '/vrm/wave_big.vrma', '/vrm/celebrate.vrma']

let _speakingEmotion = null   // last emotion received, used on speaking state entry
let _idleVarietyTimer = null

// Shared loader + clip cache (module-level singletons).
const _animLoader = new GLTFLoader()
_animLoader.register((parser) => new VRMAnimationLoaderPlugin(parser))
const _clipCache = {}
let _currentAction = null

/** loadClip fetches and caches a VRMA AnimationClip for the given VRM. */
async function loadClip(url, v) {
  const key = url + '|' + (v?.scene?.uuid ?? '')
  if (_clipCache[key]) return _clipCache[key]
  const gltf = await _animLoader.loadAsync(url)
  const anim = gltf.userData.vrmAnimations?.[0]
  if (!anim) return null
  const clip = createVRMAnimationClip(anim, v)
  _clipCache[key] = clip
  return clip
}

/**
 * playAnimation crossfades to a new VRMA clip.
 * Uses crossFadeTo() for smooth transitions and LoopPingPong to
 * eliminate the hard jump at loop boundaries.
 */
async function playAnimation(url, { loop = true, fadeTime = 0.5 } = {}) {
  if (!vrm || !idleMixer) return
  try {
    const clip = await loadClip(url, vrm)
    if (!clip) return
    const newAction = idleMixer.clipAction(clip)
    newAction.setLoop(loop ? THREE.LoopPingPong : THREE.LoopOnce, Infinity)
    newAction.clampWhenFinished = !loop
    if (_currentAction && _currentAction !== newAction) {
      newAction.reset().play()
      _currentAction.crossFadeTo(newAction, fadeTime, true)
    } else if (!_currentAction) {
      newAction.reset().play()
    }
    _currentAction = newAction
  } catch (e) {
    console.warn('playAnimation failed:', url, e)
  }
}

/** initAnimationSystem sets up the mixer and plays the welcome + idle sequence. */
async function initAnimationSystem(v) {
  if (idleMixer) { idleMixer.stopAllAction(); idleMixer = null }
  _currentAction = null
  idleMixer = new THREE.AnimationMixer(v.scene)
  // Welcome greeting on first load, then settle into idle.
  await playAnimation('/vrm/wave_big.vrma', { loop: false, fadeTime: 0.3 })
  setTimeout(() => { if (mounted) playAnimation(STATE_ANIMS.idle, { fadeTime: 0.8 }) }, 3000)
}

/** applyStateAnimation switches to the animation for the given petState. */
async function applyStateAnimation(state) {
  if (state === 'speaking') {
    const file = (EMOTION_SPEAKING_ANIMS[_speakingEmotion]) ?? STATE_ANIMS.speaking
    await playAnimation(file, { fadeTime: 0.4 })
  } else {
    const file = STATE_ANIMS[state]
    if (file) await playAnimation(file, { fadeTime: 0.5 })
  }
  // Reset emotion override when leaving speaking state.
  if (state !== 'speaking') _speakingEmotion = null
}

/**
 * scheduleIdleEvent fires a random idle event every 60–120s:
 * 40% chance → variety idle (relaxed/sleepy/idle_loop) for 15–20s
 * 60% chance → one-shot gesture (nod/wave/hello)
 */
function scheduleIdleVariety() {
  _idleVarietyTimer = setTimeout(async () => {
    if (!mounted) return
    if (petState.value === 'idle') {
      if (Math.random() < 0.4) {
        const pick = IDLE_VARIETY_POOL[Math.floor(Math.random() * IDLE_VARIETY_POOL.length)]
        await playAnimation(pick, { fadeTime: 0.8 })
        setTimeout(() => {
          if (mounted && petState.value === 'idle') playAnimation(STATE_ANIMS.idle, { fadeTime: 1.0 })
        }, 15000 + Math.random() * 5000)
      } else {
        const pick = IDLE_GESTURES[Math.floor(Math.random() * IDLE_GESTURES.length)]
        await playAnimation(pick, { loop: false, fadeTime: 0.5 })
        setTimeout(() => {
          if (mounted && petState.value === 'idle') playAnimation(STATE_ANIMS.idle, { fadeTime: 0.8 })
        }, 3500)
      }
    }
    scheduleIdleVariety()
  }, 60000 + Math.random() * 60000)
}

/** tick is the main render loop called every animation frame. */
function tick() {
  if (!mounted) return
  const dt = Math.min(clock.getDelta(), 0.1) // cap dt to avoid huge jumps
  idleMixer?.update(dt)
  if (!idleMixer) updateHeadIK(dt)
  updateMouthAnim(dt)
  updateEmotionBlend(dt)
  if (vrm) vrm.update(dt)
  renderer.render(scene, camera)
  rafId = requestAnimationFrame(tick)
}

// ── Initialization ───────────────────────────────────────────────────────────

/** initRenderer creates the THREE.js scene, camera, lights, and WebGL renderer. */
async function initRenderer() {
  scene = new THREE.Scene()
  camera = new THREE.PerspectiveCamera(40, 1, 0.1, 20)
  camera.position.set(0, 0.75, 3.5)
  camera.lookAt(0, 0.75, 0)

  renderer = new THREE.WebGLRenderer({ canvas: canvasRef.value, alpha: true, antialias: true })
  renderer.setPixelRatio(window.devicePixelRatio || 1)
  renderer.setSize(petSize.value, petSize.value)
  renderer.outputColorSpace = THREE.SRGBColorSpace

  const dirLight = new THREE.DirectionalLight(0xffffff, 1.0)
  dirLight.position.set(1, 1, 1)
  scene.add(dirLight)
  scene.add(new THREE.AmbientLight(0xffffff, 0.4))

  clock = new THREE.Clock()
  tick()
}

/** loadVRM loads a .vrm file by URL and replaces the current model in the scene. */
async function loadVRM(url) {
  if (!url) return
  const loader = new GLTFLoader()
  loader.register((parser) => new VRMLoaderPlugin(parser))
  const gltf = await loader.loadAsync(url)
  const newVrm = gltf.userData.vrm
  VRMUtils.removeUnnecessaryVertices(gltf.scene)
  VRMUtils.removeUnnecessaryJoints(gltf.scene)
  // VRM 0.x faces +Z (toward camera); VRM 1.0 follows glTF and faces -Z (away).
  // Rotate 1.0 models 180° so the front always faces the camera.
  // VRM 0.x faces -Z; VRM 1.0 faces +Z (glTF standard). Camera is at +Z.
  const isVRM0 = !!gltf.parser.json.extensions?.VRM
  newVrm.scene.rotation.y = isVRM0 ? Math.PI : 0

  if (vrm) {
    scene.remove(vrm.scene)
    VRMUtils.deepDispose(vrm.scene)
  }
  vrm = newVrm
  scene.add(vrm.scene)
  initAnimationSystem(vrm)
}

// ── Idle Animations ──────────────────────────────────────────────────────────

/** scheduleBlink randomly blinks the VRM model every 3–6 seconds. */
function scheduleBlink() {
  blinkTimer = setTimeout(async () => {
    if (!mounted || !vrm?.expressionManager) { scheduleBlink(); return }
    try {
      vrm.expressionManager.setValue('blink', 1)
      await new Promise(r => setTimeout(r, 100))
      if (!mounted) return
      vrm.expressionManager.setValue('blink', 0)
    } catch (_) {}
    scheduleBlink()
  }, 3000 + Math.random() * 3000)
}


// ── Context Menu ─────────────────────────────────────────────────────────────

/** switchToNextVRMModel cycles through available VRM models. */
async function switchToNextVRMModel() {
  const models = availableVRMModels.value
  if (models.length <= 1) return
  const idx = models.findIndex(m => m.name === currentVRMModel.value)
  const next = models[(idx + 1) % models.length]
  EventsEmit('config:vrm:model:changed', next.name)
  try {
    const cfg = await GetConfig()
    if (cfg) { cfg.VRMModel = next.name; await SaveConfig(cfg) }
  } catch (e) { console.warn('switchToNextVRMModel:', e) }
}

function onContextMenu(e) {
  e.preventDefault()
  petMenuRef.value?.show(e.clientX, e.clientY)
}

const petMenuItems = [
  { iconSvg: ICON_SHIRT,   label: '更换模型', action: switchToNextVRMModel },
  { divider: true },
  { iconSvg: ICON_SETTING, label: '打开设置', action: () => emit('open-settings') },
  { divider: true },
  { iconSvg: ICON_POWER,   label: '退出程序', action: () => Quit(), danger: true },
]

// ── Drag Handling ────────────────────────────────────────────────────────────

function onMouseDown(e) {
  if (e.button !== 0) return
  dragStart = { x: e.clientX - pos.value.x, y: e.clientY - pos.value.y, startX: e.clientX, startY: e.clientY }
  isDragging = false
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
  window.addEventListener('blur', onMouseUp)
}

function onMouseMove(e) {
  if (!dragStart || !pos.value) return
  const dx = e.clientX - dragStart.startX
  const dy = e.clientY - dragStart.startY
  if (!isDragging && Math.sqrt(dx * dx + dy * dy) < 5) return
  isDragging = true
  pos.value = { x: e.clientX - dragStart.x, y: e.clientY - dragStart.y }
  // Tilt VRM body toward drag direction.
  if (vrm) vrm.scene.rotation.y = Math.max(-0.35, Math.min(0.35, dx / 200))
}

async function onMouseUp(e) {
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  window.removeEventListener('blur', onMouseUp)
  if (isDragging) {
    if (vrm) vrm.scene.rotation.y = 0
    try { await SaveBallPosition(Math.round(pos.value.x), Math.round(pos.value.y), sw.value, sh.value) }
    catch (err) { console.error('SaveBallPosition:', err) }
  } else if (e && typeof e.clientX === 'number') {
    emit('click')
  }
  dragStart = null
  isDragging = false
}

// ── VRM Drag-Drop Import ─────────────────────────────────────────────────────

/** onDrop handles a .vrm file dragged onto the pet widget. */
async function onDrop(e) {
  const file = e.dataTransfer?.files?.[0]
  if (!file || !file.name.endsWith('.vrm')) return
  try {
    const buf = await file.arrayBuffer()
    // Convert ArrayBuffer to base64 in chunks to avoid call stack overflow on large files.
    const bytes = new Uint8Array(buf)
    let binary = ''
    const chunkSize = 8192
    for (let i = 0; i < bytes.length; i += chunkSize) {
      binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize))
    }
    const b64 = btoa(binary)
    await ImportVRMFile(file.name, b64)
    await loadVRMModels()
    EventsEmit('config:vrm:model:changed', file.name)
    const cfg = await GetConfig()
    if (cfg) { cfg.VRMModel = file.name; await SaveConfig(cfg) }
  } catch (err) {
    console.error('ImportVRMFile failed:', err)
  }
}

// ── Mouse Tracking ───────────────────────────────────────────────────────────

/** startGlobalMouseTracking polls Go for cursor position every 50ms for head IK. */
function startGlobalMouseTracking() {
  let lastX = -1, lastY = -1
  mouseTrackTimer = setInterval(async () => {
    if (!mounted || !vrm) return
    try {
      const { x, y } = await GetMousePosition()
      if (!mounted) return
      if (x === lastX && y === lastY) return
      lastX = x; lastY = y
      focusGlobal(x, y)
    } catch (_) {}
  }, MOUSE_POLL_MS)
}

// ── Lifecycle ────────────────────────────────────────────────────────────────

// Subscribe to emotion events — forwarded to applyEmotion.
useEmotionEvents(({ emotion, intensity }) => applyEmotion({ emotion, intensity }))

// Hot-reload VRM when model URL changes.
watch(vrmModelURL, async (url) => {
  if (!renderer || !mounted || !url) return
  try { await loadVRM(url) } catch (err) { console.error('VRM reload failed:', err) }
})

// Drive pet state: expressions + animation.
watch(petState, (state) => {
  setState(state)
  applyStateAnimation(state)
})

let offSizeChange, offPositionReset, offScreenChanged

onMounted(async () => {
  loadVRMModels()

  // Phase 1: load screen size + position.
  try {
    sw.value = window.innerWidth
    sh.value = window.innerHeight
    try {
      const [screenW, screenH] = await GetScreenSize()
      if (screenW > 0) { sw.value = screenW; sh.value = screenH }
    } catch (_) {}

    try {
      const size = await GetPetSize(sw.value, sh.value)
      if (size > 0) { petSize.value = size; emit('ball-size', size) }
    } catch (_) {}

    const [bx, by] = await GetBallPosition(sw.value, sh.value)
    pos.value = (bx >= 0 && by >= 0)
      ? { x: bx, y: by }
      : { x: sw.value - petSize.value - 40, y: sh.value - petSize.value - 40 }
  } catch (_) {
    pos.value = { x: window.innerWidth - petSize.value - 40, y: window.innerHeight - petSize.value - 40 }
  }

  // Phase 2: init renderer + load VRM.
  await nextTick()
  try {
    await initRenderer()
    if (vrmModelURL.value) await loadVRM(vrmModelURL.value)
    startGlobalMouseTracking()
    scheduleBlink()
    scheduleIdleVariety()
  } catch (err) {
    console.error('VRMPet init failed:', err)
  }

  offSizeChange = EventsOn('config:pet:size:changed', (size) => setSize(size))
  offPositionReset = EventsOn('ball:position:reset', () => {
    pos.value = { x: sw.value - petSize.value - 40, y: sh.value - petSize.value - 40 }
  })
  offScreenChanged = EventsOn('screen:active:changed', async (info) => {
    sw.value = info.width; sh.value = info.height
    try {
      const size = await GetPetSize(info.width, info.height)
      if (size > 0) setSize(size)
    } catch (_) {}
    try {
      const [bx, by] = await GetBallPosition(info.width, info.height)
      pos.value = (bx >= 0 && by >= 0)
        ? { x: bx, y: by }
        : { x: info.width - petSize.value - 40, y: info.height - petSize.value - 40 }
    } catch (_) {}
  })
})

onUnmounted(() => {
  mounted = false
  if (mouseTrackTimer) { clearInterval(mouseTrackTimer); mouseTrackTimer = null }
  clearTimeout(blinkTimer)
  clearTimeout(_idleVarietyTimer)
  offSizeChange?.()
  offPositionReset?.()
  offScreenChanged?.()
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
  window.removeEventListener('blur', onMouseUp)
  if (rafId) { cancelAnimationFrame(rafId); rafId = null }
  if (idleMixer) { idleMixer.stopAllAction(); idleMixer = null }
  if (vrm) { VRMUtils.deepDispose(vrm.scene); vrm = null }
  if (renderer) { renderer.dispose(); renderer = null }
  scene = null
})
</script>

<template>
  <div
    v-if="pos"
    class="vrm-pet"
    :style="{ left: pos.x + 'px', top: pos.y + 'px', width: petSize + 'px', height: petSize + 'px' }"
    @mousedown="onMouseDown"
    @contextmenu="onContextMenu"
    @dragover.prevent
    @drop.prevent="onDrop"
  >
    <canvas ref="canvasRef" class="pet-canvas" />
    <ContextMenu ref="petMenuRef" :items="petMenuItems" />
  </div>
</template>

<style scoped>
.vrm-pet {
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
