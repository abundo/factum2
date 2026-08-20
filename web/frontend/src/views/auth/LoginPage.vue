<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useLayout } from '@/layout/composables/layout'
import { useAuthStore } from '@/stores/auth'

const { layoutState, toggleDarkMode } = useLayout()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const username = ref('')
const password = ref('')
const showPassword = ref(false)
const error = ref(null)
const loading = ref(false)

function submit() {
  if (!username.value || !password.value) {
    error.value = 'Username and password are required.'
    return
  }

  loading.value = true
  error.value = null
  authStore
    .login(username.value, password.value)
    .then(() => {
      router.push(route.query.redirect ?? '/')
    })
    .catch((err) => {
      error.value = err.response?.data?.error ?? 'Login failed.'
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
  <div class="flex min-h-screen min-w-screen items-center justify-center overflow-hidden bg-muted">
    <div class="flex flex-col items-center justify-center">
      <div class="w-full rounded-2xl border border-default bg-default px-8 py-20 sm:px-20">
        <div class="mb-8 text-center">
          <div class="mb-4 text-3xl font-medium">Factum</div>
          <span class="text-muted font-medium">Sign in to continue</span>
        </div>

        <form @submit.prevent="submit">
          <label for="username" class="mb-2 block text-xl font-medium">Username</label>
          <UInput
            id="username"
            v-model="username"
            type="text"
            placeholder="Username"
            class="mb-8 w-full md:w-120"
            autofocus
          />

          <label for="password" class="mb-2 block text-xl font-medium">Password</label>
          <UInput
            id="password"
            v-model="password"
            :type="showPassword ? 'text' : 'password'"
            placeholder="Password"
            class="mb-4 w-full"
            :ui="{ trailing: 'pe-1' }"
          >
            <template #trailing>
              <UButton
                color="neutral"
                variant="link"
                size="sm"
                :icon="showPassword ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                :aria-label="showPassword ? 'Hide password' : 'Show password'"
                :aria-pressed="showPassword"
                @click="showPassword = !showPassword"
              />
            </template>
          </UInput>

          <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-4" />

          <UButton
            type="submit"
            label="Sign In"
            block
            class="mt-4"
            :loading="loading"
          />

          <div class="mt-4 text-center">
            <RouterLink to="/forgot-password" class="text-sm text-primary hover:underline"
              >Forgot password?</RouterLink
            >
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
