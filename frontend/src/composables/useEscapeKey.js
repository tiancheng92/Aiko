import { onMounted, onUnmounted, unref } from 'vue'

/**
 * useEscapeKey registers a window `keydown` listener that invokes `handler`
 * when the Escape key is pressed. `active` gates the handler — pass a ref or
 * a getter so the listener stays mounted but only fires when the owning
 * modal/menu is visible.
 */
export function useEscapeKey(handler, active = true) {
  const onKeydown = (e) => {
    if (e.key !== 'Escape') return
    const on = typeof active === 'function' ? active() : unref(active)
    if (on) handler(e)
  }
  onMounted(() => window.addEventListener('keydown', onKeydown))
  onUnmounted(() => window.removeEventListener('keydown', onKeydown))
}
