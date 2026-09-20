import { reactive } from 'vue'
import { getBranding } from '@/api/branding'

const branding = reactive({ logo: '', text: '' })
let loadPromise = null

function apply(data) {
  branding.logo = data?.logo || ''
  branding.text = data?.text || ''
}

export function loadBranding() {
  loadPromise = getBranding()
    .then((data) => {
      apply(data)
      return data
    })
    .catch(() => {
      apply({})
    })
  return loadPromise
}

export function useBranding() {
  if (!loadPromise) loadBranding()
  return { branding, reload: loadBranding }
}
