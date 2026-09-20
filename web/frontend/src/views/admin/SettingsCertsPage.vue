<script setup>
import SettingsFormPage from '@/components/SettingsFormPage.vue'

const certKeyTypeItems = [
  { label: 'EC256', value: 'EC256' },
  { label: 'EC384', value: 'EC384' },
  { label: 'RSA2048', value: 'RSA2048' },
  { label: 'RSA4096', value: 'RSA4096' },
]

function fillCertsDefaults(settings) {
  if (!settings.certs_lego_yaml) settings.certs_lego_yaml = '/var/lib/lego/.lego.yaml'
  if (!settings.certs_env_file) settings.certs_env_file = '/var/lib/lego/.env'
  if (!settings.certs_lego_bin) settings.certs_lego_bin = 'lego'
  if (!settings.certs_lego_storage) settings.certs_lego_storage = '/var/lib/lego/storage'
  if (!settings.certs_default_key_type) settings.certs_default_key_type = 'EC256'
}

function onCertsToggle(settings, val) {
  settings.certs_enabled = val
  if (val) fillCertsDefaults(settings)
}
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="Certificates">
    <div class="flex items-center gap-2">
      <USwitch
        :model-value="!!settings.certs_enabled"
        id="certs_enabled"
        @update:model-value="onCertsToggle(settings, $event)"
      />
      <label for="certs_enabled" class="font-bold">Enabled</label>
    </div>
    <small class="text-muted-color -mt-4"
      >Certificate table, ACME accounts, and DNS-01/RFC2136 challenges. Sync writes
      <span class="font-mono">.lego.yaml</span> and <span class="font-mono">.env</span>, then runs
      lego. Certificate distribution and service restarts are out of scope.</small
    >
    <div>
      <label for="certs_lego_yaml" class="block font-bold mb-3">lego YAML path</label>
      <UInput
        id="certs_lego_yaml"
        v-model="settings.certs_lego_yaml"
        placeholder="/var/lib/lego/.lego.yaml"
        class="w-full"
      />
    </div>
    <div>
      <label for="certs_env_file" class="block font-bold mb-3">.env path</label>
      <UInput
        id="certs_env_file"
        v-model="settings.certs_env_file"
        placeholder="/var/lib/lego/.env"
        class="w-full"
      />
    </div>
    <div>
      <label for="certs_lego_bin" class="block font-bold mb-3">lego binary</label>
      <UInput
        id="certs_lego_bin"
        v-model="settings.certs_lego_bin"
        placeholder="lego"
        class="w-full"
      />
    </div>
    <div>
      <label for="certs_lego_storage" class="block font-bold mb-3">lego storage dir</label>
      <UInput
        id="certs_lego_storage"
        v-model="settings.certs_lego_storage"
        placeholder="/var/lib/lego"
        class="w-full"
      />
    </div>
    <div>
      <label for="certs_default_key_type" class="block font-bold mb-3"
        >Default certificate key type</label
      >
      <USelect
        id="certs_default_key_type"
        v-model="settings.certs_default_key_type"
        :items="certKeyTypeItems"
        class="w-full"
      />
    </div>
    <div class="flex items-center gap-2">
      <USwitch
        :model-value="!!settings.certs_default_enable_common_name"
        id="certs_default_enable_common_name"
        @update:model-value="settings.certs_default_enable_common_name = $event"
      />
      <label for="certs_default_enable_common_name" class="font-bold"
        >Default enable Common Name</label
      >
    </div>
    <small class="text-muted-color -mt-4"
      >CN is deprecated in ACME. Per-certificate overrides live on each certificate row.</small
    >
  </SettingsFormPage>
</template>
