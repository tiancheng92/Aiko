import { onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'

/**
 * useEmotionEvents subscribes to the chat:emotion Wails event and forwards
 * emotion data to the pet renderer via the provided callback.
 * Automatically unsubscribes when the calling component is unmounted.
 * @param {function({emotion: string, intensity: number}): void} onEmotion
 */
export function useEmotionEvents(onEmotion) {
  const off = Events.On('chat:emotion', (event) => {
    const data = event.data
    if (data && typeof data.emotion === 'string' && typeof data.intensity === 'number') {
      onEmotion({ emotion: data.emotion, intensity: data.intensity })
    }
  })
  onUnmounted(() => { off?.() })
}
