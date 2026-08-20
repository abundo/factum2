<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useLayout } from '@/layout/composables/layout'
import { forgotPassword } from '@/api/auth'

const { layoutState, toggleDarkMode } = useLayout()
const router = useRouter()

const email = ref('')
const error = ref(null)
const loading = ref(false)
const submitted = ref(false)

function submit() {
  if (!email.value) {
    error.value = 'Email is required.'
    return
  }

  loading.value = true
  error.value = null
  forgotPassword(email.value)
    .then(() => {
      submitted.value = true
    })
    .catch((err) => {
      error.value = err.response?.data?.error ?? 'Failed to submit request.'
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
          <span class="text-muted font-medium">Reset your password</span>
        </div>

        <template v-if="submitted">
          <UAlert
            color="success"
            variant="subtle"
            title="Check your email"
            description="If that email address matches an account, we've sent a link and a code to reset your password. Click the link, or enter the code on the next page."
            class="mb-4 w-full md:w-[30rem]"
          />
          <UButton label="Enter code" block class="mb-4" @click="router.push('/reset-password')" />
          <div class="text-center">
            <RouterLink to="/login" class="text-sm text-primary hover:underline">Back to sign in</RouterLink>
          </div>
        </template>

        <form v-else @submit.prevent="submit">
          <label for="email" class="mb-2 block text-xl font-medium">Email</label>
          <UInput
            id="email"
            v-model="email"
            type="email"
            placeholder="you@example.com"
            class="mb-4 w-full md:w-[30rem]"
            autofocus
          />

          <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-4" />

          <UButton type="submit" label="Send reset link" block class="mt-4" :loading="loading" />

          <div class="mt-4 text-center">
            <RouterLink to="/login" class="text-sm text-primary hover:underline">Back to sign in</RouterLink>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
