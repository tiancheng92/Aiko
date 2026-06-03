import { onUnmounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'

/**
 * useBehaviorEvents subscribes to the chat:behavior Wails event and forwards
 * behavior data to the pet renderer via the provided callback.
 * Automatically unsubscribes when the calling component is unmounted.
 * @param {function({emotion: string, action: string}): void} onBehavior
 */
export function useBehaviorEvents(onBehavior) {
  const off = EventsOn('chat:behavior', (data) => {
    if (data && typeof data.emotion === 'string') {
      onBehavior({ emotion: data.emotion, action: data.action || '' })
    }
  })
  onUnmounted(() => { off?.() })
}
