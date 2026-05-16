import { ref, computed } from 'vue'
import { GetConfig } from '../../bindings/aiko/internal/services/configservice'
import { ListVRMModels } from '../../bindings/aiko/internal/services/systemservice'
import { Events } from '@wailsio/runtime'

const currentVRMModel = ref('')
const availableVRMModels = ref([])
let listenerRegistered = false

/** useVRMModel provides reactive VRM model state (current selection + available list). */
export function useVRMModel() {
  if (!listenerRegistered) {
    listenerRegistered = true
    Events.On('config:vrm:model:changed', async (event) => {
      const name = event.data
      // Refresh available list first so vrmModelURL can resolve the new model's URL.
      try {
        const models = await ListVRMModels()
        if (Array.isArray(models) && models.length > 0) availableVRMModels.value = models
      } catch (e) {
        console.warn('useVRMModel: failed to refresh model list', e)
      }
      currentVRMModel.value = name
    })
  }

  /**
   * vrmModelURL is the asset URL for the currently selected .vrm file.
   * Returns empty string when no model is selected.
   */
  const vrmModelURL = computed(() => {
    const m = availableVRMModels.value.find(m => m.name === currentVRMModel.value)
    return m?.url ?? ''
  })

  /** loadVRMModels fetches config and available model list from the backend. */
  async function loadVRMModels() {
    try {
      const [cfg, models] = await Promise.all([GetConfig(), ListVRMModels()])
      if (Array.isArray(models) && models.length > 0) {
        availableVRMModels.value = models
        // Use saved config value, or default to first model.
        const saved = cfg?.VRMModel
        currentVRMModel.value = (saved && models.some(m => m.name === saved))
          ? saved
          : models[0].name
      }
    } catch (e) {
      console.warn('useVRMModel: failed to load models', e)
    }
  }

  return { currentVRMModel, availableVRMModels, vrmModelURL, loadVRMModels }
}
