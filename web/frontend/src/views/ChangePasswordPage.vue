<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, reactive, ref } from 'vue'
import PageFormBar from '@/components/PageFormBar.vue'
import { getMe, updateMe } from '@/api/me'
import { usePageForm } from '@/composables/useFormGuard'

const toast = useToast()

const loading = ref(true)
const savingPassword = ref(false)
const passwordSubmitted = ref(false)

const profile = reactive({ name: '', email: '', mobile: '' })
const passwordForm = reactive({ current_password: '', new_password: '', confirm_password: '' })
const showCurrentPassword = ref(false)
const showNewPassword = ref(false)
const showConfirmPassword = ref(false)

const pageForm = computed(() => ({
  current_password: passwordForm.current_password,
  new_password: passwordForm.new_password,
  confirm_password: passwordForm.confirm_password,
}))
const { mark } = usePageForm(pageForm)

function loadProfile() {
  loading.value = true
  getMe()
    .then((data) => {
      profile.name = data.name ?? ''
      profile.email = data.email ?? ''
      profile.mobile = data.mobile ?? ''
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to load your profile.',
        duration: 3000,
      })
    })
    .finally(() => {
      loading.value = false
      mark()
    })
}

function changePassword() {
  passwordSubmitted.value = true

  if (!passwordForm.current_password || !passwordForm.new_password) {
    return
  }
  if (passwordForm.new_password !== passwordForm.confirm_password) {
    toast.add({
      color: 'error',
      title: 'Error',
      description: 'New passwords do not match.',
      duration: 3000,
    })
    return
  }

  savingPassword.value = true
  // The API replaces name/email/mobile on every update, so the current profile
  // values are sent along to avoid clobbering them with empty strings.
  updateMe({
    name: profile.name,
    email: profile.email,
    mobile: profile.mobile,
    current_password: passwordForm.current_password,
    new_password: passwordForm.new_password,
  })
    .then(() => {
      passwordForm.current_password = ''
      passwordForm.new_password = ''
      passwordForm.confirm_password = ''
      passwordSubmitted.value = false
      mark()
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Password changed',
        duration: 3000,
      })
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to change password.',
        duration: 3000,
      })
    })
    .finally(() => {
      savingPassword.value = false
    })
}

onMounted(loadProfile)
</script>

<template>
  <div>
    <PageFormBar page title="Change password">
      <UButton
        label="Change password"
        icon="i-lucide-check"
        :loading="savingPassword"
        :disabled="loading"
        @click="changePassword"
      />
    </PageFormBar>
    <div class="grid grid-cols-12 gap-8">
      <div class="col-span-12 lg:col-span-6">
        <div class="card">
          <div v-if="loading" class="flex justify-center p-4">
            <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
          </div>
          <div v-else class="flex flex-col gap-6">
            <UFormField label="Current password">
              <UInput
                id="current_password"
                v-model="passwordForm.current_password"
                :type="showCurrentPassword ? 'text' : 'password'"
                :color="passwordSubmitted && !passwordForm.current_password ? 'error' : undefined"
                :highlight="passwordSubmitted && !passwordForm.current_password"
                class="w-full"
                :ui="{ trailing: 'pe-1' }"
              >
                <template #trailing>
                  <UButton
                    color="neutral"
                    variant="link"
                    size="sm"
                    :icon="showCurrentPassword ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                    @click="showCurrentPassword = !showCurrentPassword"
                  />
                </template>
              </UInput>
            </UFormField>
            <UFormField label="New password">
              <UInput
                id="new_password"
                v-model="passwordForm.new_password"
                :type="showNewPassword ? 'text' : 'password'"
                :color="passwordSubmitted && !passwordForm.new_password ? 'error' : undefined"
                :highlight="passwordSubmitted && !passwordForm.new_password"
                class="w-full"
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
            </UFormField>
            <UFormField label="Confirm new password">
              <UInput
                id="confirm_password"
                v-model="passwordForm.confirm_password"
                :type="showConfirmPassword ? 'text' : 'password'"
                :color="
                  passwordSubmitted && passwordForm.new_password !== passwordForm.confirm_password
                    ? 'error'
                    : undefined
                "
                :highlight="
                  passwordSubmitted && passwordForm.new_password !== passwordForm.confirm_password
                "
                class="w-full"
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
            </UFormField>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
