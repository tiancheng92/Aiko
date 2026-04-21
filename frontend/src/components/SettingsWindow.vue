<!-- frontend/src/components/SettingsWindow.vue -->
<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import {
  GetConfig, SaveConfig,
  ImportKnowledge, ListKnowledgeSources, DeleteKnowledgeSource,
  OpenFileDialog, GetToolPermissions, SetToolPermission
} from '../../wailsjs/go/main/App'
import { EventsOn, EventsEmit } from '../../wailsjs/runtime/runtime'
import { useModelPath } from '../composables/useModelPath.js'

const emit = defineEmits(['close'])

const cfg = ref({
  LLMBaseURL: '', LLMAPIKey: '', LLMModel: '', EmbeddingModel: '',
  Live2DModel: 'hiyori',
  SystemPrompt: '', ShortTermLimit: 30, SkillsDir: '', Hotkey: 'Cmd+Shift+P',
  EmbeddingDim: 1536,
})
const { availableModels, loadModels } = useModelPath()
const toolPerms = ref([])   // [{ ToolName, Level, Granted }]
const sources = ref([])
const importProgress = ref(null)
const saving = ref(false)
const statusMsg = ref('')
const activeTab = ref('model')  // 'model' | 'pet' | 'tools' | 'knowledge'

// Draggable window state
const pos = ref({ x: Math.round(window.innerWidth / 2 - 300), y: Math.round(window.innerHeight / 2 - 250) })
let dragStart = null
let offProgress = null

onMounted(async () => {
  loadModels()
  const loaded = await GetConfig()
  if (loaded) Object.assign(cfg.value, loaded)
  sources.value = await ListKnowledgeSources() || []
  try { toolPerms.value = await GetToolPermissions() || [] } catch {}
  offProgress = EventsOn('knowledge:progress', (p) => { importProgress.value = p })
})

onUnmounted(() => offProgress?.())

/** save persists configuration to the backend. */
async function save() {
  saving.value = true
  statusMsg.value = ''
  try {
    await SaveConfig(cfg.value)
    EventsEmit('config:model:changed', cfg.value.Live2DModel)
    statusMsg.value = '已保存'
  } catch (e) {
    statusMsg.value = '保存失败: ' + e
  } finally {
    saving.value = false
  }
}

/** togglePerm toggles a tool permission on/off. */
async function togglePerm(perm) {
  try {
    await SetToolPermission(perm.ToolName, !perm.Granted)
    perm.Granted = !perm.Granted
  } catch (e) {
    statusMsg.value = '权限更新失败: ' + e
  }
}

/** importFile opens a file picker and imports into knowledge base. */
async function importFile() {
  const path = await OpenFileDialog('选择文档', [{ DisplayName: '文档', Pattern: '*.txt;*.md;*.pdf;*.epub' }])
  if (!path) return
  importProgress.value = { Source: path, Total: 0, Processed: 0 }
  try {
    await ImportKnowledge(path)
    sources.value = await ListKnowledgeSources() || []
  } catch (e) {
    statusMsg.value = '导入失败: ' + e
  } finally {
    importProgress.value = null
  }
}

/** deleteSource removes a knowledge source. */
async function deleteSource(src) {
  try {
    await DeleteKnowledgeSource(src)
    sources.value = sources.value.filter(s => s !== src)
  } catch (e) {
    statusMsg.value = '删除失败: ' + e
  }
}

/** onHeaderMouseDown starts window drag. */
function onHeaderMouseDown(e) {
  dragStart = { mx: e.clientX - pos.value.x, my: e.clientY - pos.value.y }
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}

/** onMouseMove updates window position during drag. */
function onMouseMove(e) {
  if (!dragStart) return
  pos.value = { x: e.clientX - dragStart.mx, y: e.clientY - dragStart.my }
}

/** onMouseUp ends the drag. */
function onMouseUp() {
  dragStart = null
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
}
</script>

<template>
  <div class="settings-win" :style="{ left: pos.x + 'px', top: pos.y + 'px' }">
    <!-- Draggable title bar -->
    <div class="win-titlebar" @mousedown="onHeaderMouseDown">
      <span class="win-title">设置</span>
      <button class="win-close" @click="$emit('close')">✕</button>
    </div>

    <!-- Sidebar + content -->
    <div class="win-body">
      <nav class="win-sidebar">
        <button :class="{ active: activeTab === 'model' }" @click="activeTab = 'model'">🤖 模型</button>
        <button :class="{ active: activeTab === 'pet' }" @click="activeTab = 'pet'">🐾 宠物</button>
        <button :class="{ active: activeTab === 'tools' }" @click="activeTab = 'tools'">🔧 工具权限</button>
        <button :class="{ active: activeTab === 'knowledge' }" @click="activeTab = 'knowledge'">📚 知识库</button>
      </nav>

      <div class="win-content">
        <!-- 模型设置 -->
        <div v-if="activeTab === 'model'" class="tab-pane">
          <label>Base URL<input v-model="cfg.LLMBaseURL" placeholder="http://localhost:11434/v1" /></label>
          <label>API Key<input v-model="cfg.LLMAPIKey" type="password" placeholder="（可选）" /></label>
          <label>Model<input v-model="cfg.LLMModel" placeholder="qwen2.5:7b" /></label>
          <label>Embedding Model<input v-model="cfg.EmbeddingModel" placeholder="nomic-embed-text（可选）" /></label>
          <label>Embedding 维度<input type="number" v-model.number="cfg.EmbeddingDim" min="256" max="4096" /></label>
        </div>

        <!-- 宠物设置 -->
        <div v-if="activeTab === 'pet'" class="tab-pane">
          <label>Live2D 模型
            <div class="model-grid">
              <button
                v-for="m in availableModels"
                :key="m"
                :class="['model-btn', { selected: cfg.Live2DModel === m }]"
                @click="cfg.Live2DModel = m"
              >{{ m }}</button>
            </div>
          </label>
          <label>System Prompt<textarea v-model="cfg.SystemPrompt" rows="5" /></label>
          <label>短期记忆轮数（1-100）<input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" /></label>
          <label>Skills 目录<input v-model="cfg.SkillsDir" placeholder="~/.desktop-pet/skills" /></label>
        </div>

        <!-- 工具权限 -->
        <div v-if="activeTab === 'tools'" class="tab-pane">
          <p class="hint">Public 工具无需授权；Protected 工具需要手动开启。</p>
          <div v-if="toolPerms.length === 0" class="empty">暂无工具信息</div>
          <div v-for="perm in toolPerms" :key="perm.ToolName" class="perm-row">
            <div class="perm-info">
              <span class="perm-name">{{ perm.ToolName }}</span>
              <span :class="['perm-level', perm.Level]">{{ perm.Level }}</span>
            </div>
            <label class="toggle">
              <input type="checkbox" :checked="perm.Granted" :disabled="perm.Level === 'public'" @change="togglePerm(perm)" />
              <span class="toggle-track" />
            </label>
          </div>
        </div>

        <!-- 知识库 -->
        <div v-if="activeTab === 'knowledge'" class="tab-pane">
          <button @click="importFile" :disabled="!!importProgress">导入文件</button>
          <div v-if="importProgress" class="progress">
            {{ importProgress.Source }}: {{ importProgress.Processed }}/{{ importProgress.Total }}
          </div>
          <ul v-if="sources.length">
            <li v-for="src in sources" :key="src">
              <span>{{ src }}</span>
              <button @click="deleteSource(src)">删除</button>
            </li>
          </ul>
          <p v-else class="empty">暂无知识库文件</p>
        </div>
      </div>
    </div>

    <!-- Footer -->
    <div class="win-footer">
      <span class="status-msg">{{ statusMsg }}</span>
      <button @click="save" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
    </div>
  </div>
</template>

<style scoped>
.settings-win {
  position: fixed;
  z-index: 99990;
  width: 600px;
  height: 500px;
  background: #111827;
  border: 1px solid #374151;
  border-radius: 12px;
  box-shadow: 0 16px 48px rgba(0,0,0,0.7);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-size: 13px;
  color: #e5e7eb;
}
.win-titlebar {
  display: flex;
  align-items: center;
  background: #1f2937;
  border-bottom: 1px solid #374151;
  padding: 0 12px;
  height: 38px;
  cursor: move;
  flex-shrink: 0;
  user-select: none;
}
.win-title { flex: 1; font-weight: 600; font-size: 13px; }
.win-close { background: none; border: none; color: #6b7280; cursor: pointer; font-size: 14px; padding: 4px 6px; }
.win-close:hover { color: #ef4444; }
.win-body { flex: 1; display: flex; overflow: hidden; }
.win-sidebar { width: 120px; background: #1a2332; border-right: 1px solid #374151; display: flex; flex-direction: column; padding: 8px 0; flex-shrink: 0; }
.win-sidebar button { background: none; border: none; color: #9ca3af; padding: 9px 14px; cursor: pointer; font-size: 12px; text-align: left; }
.win-sidebar button:hover { background: #374151; color: #f9fafb; }
.win-sidebar button.active { background: #374151; color: #f9fafb; border-right: 2px solid #4f46e5; }
.win-content { flex: 1; overflow-y: auto; padding: 16px; }
.tab-pane { display: flex; flex-direction: column; gap: 10px; }
label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: #9ca3af; }
input, textarea { background: #1f2937; border: 1px solid #374151; border-radius: 6px; padding: 6px 10px; color: #f9fafb; font-size: 13px; outline: none; font-family: inherit; }
input:focus, textarea:focus { border-color: #4f46e5; }
textarea { resize: vertical; }
.hint { color: #6b7280; font-size: 12px; margin: 0 0 8px; }
.perm-row { display: flex; justify-content: space-between; align-items: center; padding: 8px 0; border-bottom: 1px solid #1f2937; }
.perm-info { display: flex; align-items: center; gap: 8px; }
.perm-name { font-size: 13px; }
.perm-level { font-size: 10px; padding: 2px 6px; border-radius: 4px; }
.perm-level.public { background: #065f46; color: #6ee7b7; }
.perm-level.protected { background: #7c2d12; color: #fdba74; }
.toggle { display: flex; align-items: center; cursor: pointer; }
.toggle input { display: none; }
.toggle-track { width: 36px; height: 20px; background: #374151; border-radius: 10px; position: relative; transition: background 0.2s; }
.toggle input:checked ~ .toggle-track { background: #4f46e5; }
.toggle-track::after { content:''; position:absolute; top:2px; left:2px; width:16px; height:16px; background:#fff; border-radius:50%; transition: transform 0.2s; }
.toggle input:checked ~ .toggle-track::after { transform: translateX(16px); }
.toggle input:disabled ~ .toggle-track { opacity: 0.4; cursor: not-allowed; }
button { background: #4f46e5; color: #fff; border: none; border-radius: 6px; padding: 6px 14px; cursor: pointer; font-size: 13px; }
button:disabled { opacity: 0.5; cursor: not-allowed; }
ul { list-style: none; padding: 0; margin-top: 6px; }
li { display: flex; justify-content: space-between; align-items: center; padding: 4px 0; border-bottom: 1px solid #1f2937; }
li button { background: #374151; padding: 3px 10px; font-size: 12px; }
li button:hover { background: #dc2626; }
.empty { color: #6b7280; font-size: 12px; margin-top: 4px; }
.progress { color: #9ca3af; font-size: 12px; margin: 6px 0; }
.win-footer { display: flex; justify-content: flex-end; align-items: center; gap: 10px; padding: 10px 16px; border-top: 1px solid #374151; flex-shrink: 0; background: #111827; }
.status-msg { color: #6b7280; font-size: 12px; }
.model-grid { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 4px; }
.model-btn { background: #1f2937; color: #9ca3af; border: 1px solid #374151; border-radius: 6px; padding: 6px 14px; cursor: pointer; font-size: 12px; }
.model-btn:hover { border-color: #4f46e5; color: #f9fafb; }
.model-btn.selected { background: #312e81; border-color: #4f46e5; color: #a5b4fc; }
</style>
