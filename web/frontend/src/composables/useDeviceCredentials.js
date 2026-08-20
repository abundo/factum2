import { ref } from 'vue'
import { useDeviceCredentialsStore } from '@/stores/deviceCredentials'

/**
 * Prompt + cache helpers for device SSH credentials. Shared across
 * DeviceList, ServiceEditDialog, and VlanEditDialog so one successful
 * login is reused everywhere in the tab (sessionStorage-backed store).
 *
 * Usage:
 *   const {
 *     credentialsDialog, promptUsername, promptPassword,
 *     withCredentials, submitCredentials, cancelCredentials,
 *     rememberSuccess, rememberFailure,
 *   } = useDeviceCredentials()
 *
 *   withCredentials(deviceId, (username, password) => { ... })
 *   // multi-device (ELINE): withCredentials([idA, idB], action)
 */
export function useDeviceCredentials() {
  const store = useDeviceCredentialsStore()

  const credentialsDialog = ref(false)
  const promptUsername = ref('')
  const promptPassword = ref('')
  const pendingAction = ref(null)

  function withCredentials(deviceIds, action) {
    const creds = store.getForDevices(deviceIds)
    if (creds) {
      action(creds.username, creds.password)
      return
    }
    pendingAction.value = action
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
    credentialsDialog.value = false
    pendingAction.value = null
    action?.(username, password)
  }

  function cancelCredentials() {
    credentialsDialog.value = false
    pendingAction.value = null
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
