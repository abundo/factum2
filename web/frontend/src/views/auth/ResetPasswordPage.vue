<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useLayout } from '@/layout/composables/layout'
import { resetPassword } from '@/api/auth'

const { layoutState, toggleDarkMode } = useLayout()
const route = useRoute()
const router = useRouter()

// Arriving via the emailed link (?token=...) redeems by token; otherwise
// the user enters the email + short code from the same email instead -
// either one redeems the same request (see web.ApiResetPassword). Fixed
// for the page's lifetime, based on how it was reached.
const useToken = !!route.query.token
const token = route.query.token ?? ''
const email = ref('')
const code = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const showNewPassword = ref(false)
const showConfirmPassword = ref(false)
const error = ref(null)
const loading = ref(false)
const success = ref(false)

function submit() {
  error.value = null

  if (useToken && !token) {
    error.value = 'Reset link is missing its token.'
    return
  }
  if (!useToken && (!email.value || !code.value)) {
    error.value = 'Email and code are required.'
    return
  }
  if (!newPassword.value) {
    error.value = 'New password is required.'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    error.value = 'New passwords do not match.'
    return
  }

  const payload = useToken
    ? { token, new_password: newPassword.value }
    : { email: email.value, code: code.value, new_password: newPassword.value }

  loading.value = true
  resetPassword(payload)
    .then(() => {
      success.value = true
    })
    .catch((err) => {
      error.value = err.response?.data?.error ?? 'Failed to reset password.'
    })
    .finally(() => {
      loading.value = false
    })
}
</script>

<template>
  <div class="fixed right-8 top-8">
    <UButton
      :icon="layoutState.darkTheme ? 'i-lucide-moon' : 'i-lucide-sun'"
      variant="soft"
      color="neutral"
      size="lg"
      square
      @click="toggleDarkMode"
    />
  </div>
  <div class="flex min-h-screen min-w-[100vw] items-center justify-center overflow-hidden bg-muted">
    <div class="flex flex-col items-center justify-center">
      <div class="w-full rounded-2xl border border-default bg-default px-8 py-20 sm:px-20">
        <div class="mb-8 text-center">
          <div class="mb-4 text-3xl font-medium">Factum</div>
          <span class="text-muted font-medium">Choose a new password</span>
        </div>

        <template v-if="success">
          <UAlert
            color="success"
            variant="subtle"
            title="Password changed"
            description="Your password has been reset. You can now sign in with it."
            class="mb-4 w-full md:w-[30rem]"
          />
          <UButton label="Go to sign in" block @click="router.push('/login')" />
        </template>

        <form v-else class="w-full md:w-[30rem]" @submit.prevent="submit">
          <template v-if="!useToken">
            <label for="email" class="mb-2 block text-xl font-medium">Email</label>
            <UInput
              id="email"
              v-model="email"
              type="email"
              placeholder="you@example.com"
              class="mb-6 w-full"
              autofocus
            />

            <label for="code" class="mb-2 block text-xl font-medium">Code</label>
            <UInput id="code" v-model="code" placeholder="12345678" class="mb-6 w-full" />
          </template>

          <label for="new_password" class="mb-2 block text-xl font-medium">New password</label>
          <UInput
            id="new_password"
            v-model="newPassword"
            :type="showNewPassword ? 'text' : 'password'"
            class="mb-6 w-full"
            :ui="{ trailing: 'pe-1' }"
          >
            <template #trailing>
              <UButton
                color="neutral"
                variant="link"
                size="sm"
                :icon="showNewPassword ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                @click="showNewPassword = !showNewPassword"
              />
            </template>
          </UInput>

          <label for="confirm_password" class="mb-2 block text-xl font-medium">Confirm new password</label>
          <UInput
            id="confirm_password"
            v-model="confirmPassword"
            :type="showConfirmPassword ? 'text' : 'password'"
            class="mb-4 w-full"
            :ui="{ trailing: 'pe-1' }"
          >
            <template #trailing>
              <UButton
                color="neutral"
                variant="link"
                size="sm"
                :icon="showConfirmPassword ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                @click="showConfirmPassword = !showConfirmPassword"
              />
            </template>
          </UInput>

          <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-4" />

          <UButton type="submit" label="Reset password" block class="mt-4" :loading="loading" />

          <div class="mt-4 text-center">
            <RouterLink to="/forgot-password" class="text-sm text-primary hover:underline"
              >Request a new link or code</RouterLink
            >
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
