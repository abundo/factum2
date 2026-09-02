import { useToast } from '@nuxt/ui/composables'
import { onMounted, reactive, ref } from 'vue'
import { getSettings, updateSettings } from '@/api/settings'
import { useAuthStore } from '@/stores/auth'

// Shared load/save logic for the admin settings pages (Sources,
// Destinations, Factum, Device sync) - they all read and write the same
// single `Settings` row, just render a different subset of its fields.
export function useSettings() {
  const toast = useToast()

  const settings = reactive({})
  const loading = ref(true)
  const saving = ref(false)
  const forbidden = ref(false)
  const loadError = ref(false)

  function load() {
    loading.value = true
    forbidden.value = false
    loadError.value = false
    getSettings()
      .then((data) => {
        Object.assign(settings, data)
      })
      .catch((err) => {
        if (err.response?.status === 403 || err.response?.status === 401) {
          forbidden.value = true
        } else {
          loadError.value = true
        }
      })
      .finally(() => {
        loading.value = false
      })
  }

  function save() {
    saving.value = true
    updateSettings(settings)
      .then((data) => {
        Object.assign(settings, data)
        toast.add({
          color: 'success',
          title: 'Successful',
          description: 'Settings saved',
          duration: 3000,
        })
        // Feature flags (optical/ipam/organization/oxidized) live on /api/me
        // and gate the sidebar — refresh so the menu updates without a reload.
        useAuthStore().fetchCurrentUser()
      })
      .catch((err) => {
        toast.add({
          color: 'error',
          title: 'Error',
          description: err.response?.data?.error ?? 'Failed to save settings.',
          duration: 3000,
        })
      })
      .finally(() => {
        saving.value = false
      })
  }

  onMounted(load)

  return { settings, loading, saving, forbidden, loadError, load, save }
}
