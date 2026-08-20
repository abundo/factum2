<script setup>
import { useSettings } from '@/composables/useSettings'
import PasswordInput from '@/components/PasswordInput.vue'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const { settings, loading, saving, forbidden, loadError, save: saveSettings } = useSettings()

function save() {
  saveSettings()
  setTimeout(() => authStore.fetchCurrentUser(), 400)
}
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view settings."
    />
  </div>
  <div v-else-if="loadError" class="card">
    <UAlert color="error" variant="subtle" title="Failed to load settings." />
  </div>
  <div v-else class="card">
    <div class="flex justify-end mb-6">
      <UButton
        label="Save"
        icon="i-lucide-check"
        :loading="saving"
        :disabled="loading"
        @click="save"
      />
    </div>

    <div v-if="loading" class="flex justify-center p-4">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
    </div>

    <template v-else>
      <div class="font-semibold text-lg mb-3">Factum</div>
      <div class="flex flex-col gap-6">
        <div class="flex items-center gap-2">
          <USwitch
            :model-value="!!settings.optical_enabled"
            id="optical_enabled"
            @update:model-value="settings.optical_enabled = $event"
          />
          <label for="optical_enabled" class="font-bold">Optical / WDM modeling</label>
        </div>
        <small class="text-muted-color -mt-4"
          >ROADM, transponder/muxponder, wavelength and dark-fiber paths, maintenance impact. Off by
          default — packet-only deployments never see those screens.</small
        >
        <div class="flex items-center gap-2">
          <USwitch
            :model-value="!!settings.ipam_enabled"
            id="ipam_enabled"
            @update:model-value="settings.ipam_enabled = $event"
          />
          <label for="ipam_enabled" class="font-bold">IP address management</label>
        </div>
        <small class="text-muted-color -mt-4"
          >Namespaces, VRFs and prefix allocation. Off by default. Turning this off hides the UI; it
          does not delete any IPAM data.</small
        >
        <div>
          <label for="factum_api_token" class="block font-bold mb-3">API token</label>
          <PasswordInput id="factum_api_token" v-model="settings.factum_api_token" />
        </div>
        <div>
          <label for="default_domain" class="block font-bold mb-3">Default domain</label>
          <UInput id="default_domain" v-model="settings.default_domain" class="w-full" />
        </div>
        <div>
          <label for="public_base_url" class="block font-bold mb-3">Public URL</label>
          <UInput
            id="public_base_url"
            v-model="settings.public_base_url"
            placeholder="https://factum.example.com"
            class="w-full"
          />
          <small class="text-muted-color"
            >Used to build absolute links in outgoing email, e.g. the password-reset link. If left
            blank, the incoming request's host is used instead.</small
          >
        </div>
      </div>
    </template>
  </div>
</template>
