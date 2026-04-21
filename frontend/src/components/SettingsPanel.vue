<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { GetConfig, SaveConfig, ImportKnowledge, ListKnowledgeSources, DeleteKnowledgeSource } from '../../wailsjs/go/main/App'
import { EventsOn, OpenFileDialog } from '../../wailsjs/runtime/runtime'

const emit = defineEmits(['saved'])
const cfg = ref({
  LLMBaseURL: '', LLMAPIKey: '', LLMModel: '', EmbeddingModel: '',
  SystemPrompt: '', ShortTermLimit: 30, SkillsDir: '', Hotkey: 'Cmd+Shift+P',
  BallPositionX: -1, BallPositionY: -1, EmbeddingDim: 1536
})
const sources = ref([])
const importProgress = ref(null)
const saving = ref(false)
const statusMsg = ref('')

onMounted(async () => {
  const loaded = await GetConfig()
  if (loaded) Object.assign(cfg.value, loaded)
  sources.value = await ListKnowledgeSources() || []
})

const offProgress = EventsOn('knowledge:progress', (p) => { importProgress.value = p })
onUnmounted(() => offProgress())

/** save persists the current config and emits saved on success. */
async function save() {
  saving.value = true
  statusMsg.value = ''
  try {
    await SaveConfig(cfg.value)
    statusMsg.value = '已保存'
    emit('saved')
  } catch (e) {
    statusMsg.value = '保存失败: ' + e
  } finally {
    saving.value = false
  }
}

/** importFile opens a file picker and imports the selected document into the knowledge base. */
async function importFile() {
  const path = await OpenFileDialog({
    Filters: [{ DisplayName: '文档', Pattern: '*.txt;*.md;*.pdf;*.epub' }]
  })
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

/** deleteSource removes a knowledge source by name. */
async function deleteSource(src) {
  try {
    await DeleteKnowledgeSource(src)
    sources.value = sources.value.filter(s => s !== src)
  } catch (e) {
    statusMsg.value = '删除失败: ' + e
  }
}
</script>

<template>
  <div class="settings-panel">
    <div class="section">
      <h3>模型设置</h3>
      <label>Base URL<input v-model="cfg.LLMBaseURL" placeholder="http://localhost:11434/v1" /></label>
      <label>API Key<input v-model="cfg.LLMAPIKey" placeholder="（可选）" type="password" /></label>
      <label>Model<input v-model="cfg.LLMModel" placeholder="qwen2.5:7b" /></label>
      <label>Embedding Model<input v-model="cfg.EmbeddingModel" placeholder="nomic-embed-text（可选）" /></label>
    </div>
    <div class="section">
      <h3>宠物设置</h3>
      <label>System Prompt<textarea v-model="cfg.SystemPrompt" rows="3" /></label>
    </div>
    <div class="section">
      <h3>记忆设置</h3>
      <label>短期记忆轮数（1-100）
        <input type="number" v-model.number="cfg.ShortTermLimit" min="1" max="100" />
      </label>
    </div>
    <div class="section">
      <h3>快捷键</h3>
      <label>全局快捷键<input v-model="cfg.Hotkey" placeholder="Cmd+Shift+P" /></label>
    </div>
    <div class="section">
      <h3>Skills 目录</h3>
      <label>路径<input v-model="cfg.SkillsDir" placeholder="~/.desktop-pet/skills" /></label>
    </div>
    <div class="section">
      <h3>知识库</h3>
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
    <div class="actions">
      <span class="status-msg">{{ statusMsg }}</span>
      <button @click="save" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
    </div>
  </div>
</template>

<style scoped>
.settings-panel { padding: 12px; overflow-y: auto; height: 100%; font-size: 13px; color: #e5e7eb; }
.section { margin-bottom: 14px; }
h3 { font-size: 11px; text-transform: uppercase; color: #9ca3af; margin-bottom: 8px; letter-spacing: 0.05em; }
label { display: flex; flex-direction: column; gap: 3px; margin-bottom: 8px; font-size: 12px; color: #9ca3af; }
input, textarea { background: #1f2937; border: 1px solid #374151; border-radius: 6px; padding: 6px 10px; color: #f9fafb; font-size: 13px; outline: none; font-family: inherit; }
input:focus, textarea:focus { border-color: #4f46e5; }
textarea { resize: vertical; }
button { background: #4f46e5; color: #fff; border: none; border-radius: 6px; padding: 6px 14px; cursor: pointer; font-size: 13px; }
button:disabled { opacity: 0.5; cursor: not-allowed; }
.actions { display: flex; justify-content: flex-end; align-items: center; gap: 10px; padding-top: 10px; border-top: 1px solid #374151; position: sticky; bottom: 0; background: #111827; }
.status-msg { color: #6b7280; font-size: 12px; }
ul { list-style: none; padding: 0; margin-top: 6px; }
li { display: flex; justify-content: space-between; align-items: center; padding: 4px 0; border-bottom: 1px solid #1f2937; }
li button { background: #374151; padding: 3px 10px; font-size: 12px; }
li button:hover { background: #dc2626; }
.empty { color: #6b7280; font-size: 12px; margin-top: 4px; }
.progress { color: #9ca3af; font-size: 12px; margin: 6px 0; }
</style>
