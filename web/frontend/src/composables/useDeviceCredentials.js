import { ref } from 'vue'
import { useDeviceCredentialsStore } from '@/stores/deviceCredentials'

/**
 * Prompt + cache helpers for device SSH credentials. Shared across
 * DeviceList and VlanEditDialog so one successful login is reused
 * everywhere in the tab (sessionStorage-backed store). Service push/delete
 * uses Admin → Device sync credentials on the server, not this cache.
 *
 * Usage:
 *   const {
 *     credentialsDialog, promptUsername, promptPassword,
 *     withCredentials, submitCredentials, cancelCredentials,
 *     rememberSuccess, rememberFailure,
 *   } = useDeviceCredentials()
 *
 *   withCredentials(deviceId, (username, password) => { ... })
 */
export function useDeviceCredentials() {
  const store = useDeviceCredentialsStore()

  const credentialsDialog = ref(false)
  const promptUsername = ref('')
  const promptPassword = ref('')
  const pendingAction = ref(null)
  const pendingCancel = ref(null)

  function withCredentials(deviceIds, action, onCancel) {
    const creds = store.getForDevices(deviceIds)
    if (creds) {
      action(creds.username, creds.password)
      return
    }
    pendingAction.value = action
    pendingCancel.value = onCancel || null
    // Prefill username from any previous success so the operator only
    // retypes the password when switching environments.
    promptUsername.value = store.usernameHint()
    promptPassword.value = ''
    credentialsDialog.value = true
  }

  function submitCredentials() {
    if (!promptUsername.value || !promptPassword.value) return
    const action = pendingAction.value
    const username = promptUsername.value
    const password = promptPassword.value
    pendingAction.value = null
    pendingCancel.value = null
    credentialsDialog.value = false
    action?.(username, password)
  }

  function cancelCredentials() {
    const cancel = pendingCancel.value
    pendingAction.value = null
    pendingCancel.value = null
    credentialsDialog.value = false
    cancel?.()
  }

  function rememberSuccess(deviceIds, username, password) {
    store.remember(deviceIds, username, password)
  }

  function rememberFailure(deviceIds, username, password) {
    store.invalidate(deviceIds, username, password)
  }

  return {
    credentialsDialog,
    promptUsername,
    promptPassword,
    withCredentials,
    submitCredentials,
    cancelCredentials,
    rememberSuccess,
    rememberFailure,
  }
}
