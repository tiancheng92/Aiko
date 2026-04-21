import { ref, computed } from 'vue'
import { GetConfig, GetAvailableModels } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const currentModel = ref('hiyori')
const availableModels = ref([])

/** Capitalize uppercases the first character of a string. */
function capitalize(s) {
  if (!s) return s
  return s.charAt(0).toUpperCase() + s.slice(1)
}

EventsOn('config:model:changed', (name) => {
  currentModel.value = name
})

/** useModelPath provides reactive Live2D model state. */
export function useModelPath() {
  /** modelPath is the full URL path to the model3.json file. */
  const modelPath = computed(
    () => `/live2d/${currentModel.value}/${capitalize(currentModel.value)}.model3.json`
  )

  /** loadModels fetches saved config and available models from the backend. */
  async function loadModels() {
    try {
      const [cfg, models] = await Promise.all([GetConfig(), GetAvailableModels()])
      if (cfg?.Live2DModel) currentModel.value = cfg.Live2DModel
      if (Array.isArray(models) && models.length > 0) availableModels.value = models
    } catch (e) {
      console.warn('useModelPath: failed to load models', e)
    }
  }

  return { currentModel, availableModels, modelPath, loadModels }
}
