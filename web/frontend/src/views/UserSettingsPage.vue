<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, reactive, ref } from 'vue'
import PageFormBar from '@/components/PageFormBar.vue'
import { getMe, updateMe } from '@/api/me'
import { usePageForm } from '@/composables/useFormGuard'

const toast = useToast()

const loading = ref(true)
const savingProfile = ref(false)

const profile = reactive({ username: '', name: '', email: '', mobile: '' })
const profilePassword = ref('')

const pageForm = computed(() => ({
  username: profile.username,
  name: profile.name,
  email: profile.email,
  mobile: profile.mobile,
  profilePassword: profilePassword.value,
}))
const { mark } = usePageForm(pageForm)

function loadProfile() {
  loading.value = true
  getMe()
    .then((data) => {
      Object.assign(profile, data)
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

function saveProfile() {
  savingProfile.value = true
  updateMe({
    name: profile.name,
    email: profile.email,
    mobile: profile.mobile,
    current_password: profilePassword.value,
  })
    .then((data) => {
      profilePassword.value = ''
      Object.assign(profile, data)
      mark()
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Profile updated',
        duration: 3000,
      })
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to update profile.',
        duration: 3000,
      })
    })
    .finally(() => {
      savingProfile.value = false
    })
}

onMounted(loadProfile)
</script>

<template>
  <div>
    <PageFormBar page title="Profile">
      <UButton
        label="Save"
        icon="i-lucide-check"
        :loading="savingProfile"
        :disabled="loading"
        @click="saveProfile"
      />
    </PageFormBar>
    <div class="grid grid-cols-12 gap-8">
      <div class="col-span-12 lg:col-span-6">
        <div class="card">
          <div v-if="loading" class="flex justify-center p-4">
            <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
          </div>
          <div v-else class="flex flex-col gap-6">
            <UFormField label="Username">
              <UInput id="username" v-model="profile.username" disabled class="w-full" />
            </UFormField>
            <UFormField label="Name">
              <UInput id="name" v-model="profile.name" class="w-full" />
            </UFormField>
            <UFormField label="Email">
              <UInput id="email" v-model="profile.email" class="w-full" />
            </UFormField>
            <UFormField label="Mobile">
              <UInput id="mobile" v-model="profile.mobile" class="w-full" />
            </UFormField>
            <UFormField label="Current password" hint="Required when you change the email address.">
              <UInput
                id="profile_current_password"
                v-model="profilePassword"
                type="password"
                class="w-full"
                autocomplete="current-password"
              />
            </UFormField>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
