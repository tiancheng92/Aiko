// frontend/src/composables/usePetState.js
import { ref, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'

/**
 * usePetState provides reactive pet state management driven by backend events.
 * State values: 'idle' | 'thinking' | 'speaking' | 'listening' | 'error'
 */
export function usePetState() {
  const petState = ref('idle')
  let offState = null
  let errorTimer = null

  onMounted(() => {
    offState = EventsOn('pet:state:change', (state) => {
      // Clear any pending error-reset timer
      if (errorTimer) {
        clearTimeout(errorTimer)
        errorTimer = null
      }
      petState.value = state
      // 'error' state auto-resets to 'idle' after 3 seconds
      if (state === 'error') {
        errorTimer = setTimeout(() => {
          petState.value = 'idle'
          errorTimer = null
        }, 3000)
      }
    })
  })

  onUnmounted(() => {
    offState?.()
    if (errorTimer) clearTimeout(errorTimer)
  })

  return { petState }
}
