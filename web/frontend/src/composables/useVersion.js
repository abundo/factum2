import { ref } from 'vue'
import { getVersion } from '@/api/version'

const info = ref(null)
let loadPromise = null

function loadVersion() {
  if (info.value != null || loadPromise) return loadPromise
  loadPromise = getVersion()
    .then((data) => {
      info.value = data
      return data
    })
    .catch(() => {
      info.value = null
    })
    .finally(() => {
      loadPromise = null
    })
  return loadPromise
}

export function useVersion() {
  loadVersion()
  return { info }
}
