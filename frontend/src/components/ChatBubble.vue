<script setup>
import ChatPanel from './ChatPanel.vue'
import SettingsPanel from './SettingsPanel.vue'

const props = defineProps({ tab: String })
const emit = defineEmits(['update:tab', 'close'])

/** setTab switches the active tab. */
function setTab(t) { emit('update:tab', t) }

/** onSaved handles settings save by switching back to chat. */
function onSaved() { emit('update:tab', 'chat') }
</script>

<template>
  <div class="chat-bubble">
    <div class="tab-bar">
      <button :class="{ active: tab === 'chat' }" @click="setTab('chat')">聊天</button>
      <button :class="{ active: tab === 'settings' }" @click="setTab('settings')">设置</button>
      <button class="close-btn" @click="$emit('close')">✕</button>
    </div>
    <div class="content">
      <ChatPanel v-if="tab === 'chat'" />
      <SettingsPanel v-else @saved="onSaved" />
    </div>
  </div>
</template>

<style scoped>
.chat-bubble {
  position: fixed;
  bottom: 100px;
  right: 24px;
  width: clamp(320px, 22vw, 480px);
  height: clamp(360px, 55vh, 620px);
  background: #111827;
  border-radius: 16px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.5);
  display: flex;
  flex-direction: column;
  z-index: 9998;
  overflow: hidden;
}
.tab-bar {
  display: flex;
  background: #1f2937;
  border-bottom: 1px solid #374151;
  padding: 0 8px;
  flex-shrink: 0;
}
.tab-bar button {
  background: none;
  border: none;
  color: #9ca3af;
  padding: 10px 14px;
  cursor: pointer;
  font-size: 13px;
}
.tab-bar button.active { color: #f9fafb; border-bottom: 2px solid #4f46e5; }
.close-btn { margin-left: auto; color: #6b7280 !important; }
.content { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
</style>
