import { ref } from 'vue'

/**
 * useConfirm provides a minimal promise-based confirm dialog state container.
 *
 * Usage:
 *   const confirm = useConfirm()
 *   if (await confirm.ask({ title: '删除', message: '确认删除？', variant: 'danger' })) { ... }
 *   // In template: <ConfirmDialog :state="confirm" />
 */
export function useConfirm() {
  const visible = ref(false)
  const title = ref('')
  const message = ref('')
  const confirmText = ref('确认')
  const cancelText = ref('取消')
  const variant = ref('default') // 'default' | 'danger'
  let resolver = null

  function ask(opts = {}) {
    title.value = opts.title || '确认'
    message.value = opts.message || ''
    confirmText.value = opts.confirmText || '确认'
    cancelText.value = opts.cancelText || '取消'
    variant.value = opts.variant || 'default'
    visible.value = true
    return new Promise(resolve => { resolver = resolve })
  }

  function resolve(ok) {
    visible.value = false
    resolver?.(ok)
    resolver = null
  }

  return { visible, title, message, confirmText, cancelText, variant, ask, resolve }
}
