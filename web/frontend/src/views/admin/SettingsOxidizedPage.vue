<script setup>
import PasswordInput from '@/components/PasswordInput.vue'
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="Oxidized">
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.oxidized_enabled" id="oxidized_enabled" />
      <label for="oxidized_enabled" class="font-bold">Enabled</label>
    </div>
    <div>
      <label for="oxidized_api_url" class="block font-bold mb-3">API URL</label>
      <UInput id="oxidized_api_url" v-model="settings.oxidized_api_url" class="w-full" />
      <p class="text-muted-color mt-1">
        oxidized-web REST API, used by
        <span class="font-mono">factum2-oxidized</span>
        and the Oxidized device browser. The browser runs on this factum-web host, so the URL must
        be reachable from here (not only
        <span class="font-mono">127.0.0.1</span>
        on the Oxidized server).
      </p>
    </div>
    <div>
      <label for="oxidized_api_user" class="block font-bold mb-3">API User</label>
      <UInput id="oxidized_api_user" v-model="settings.oxidized_api_user" class="w-full" />
    </div>
    <div>
      <label for="oxidized_api_pass" class="block font-bold mb-3">API Password</label>
      <PasswordInput id="oxidized_api_pass" v-model="settings.oxidized_api_pass" />
    </div>
    <div>
      <label for="oxidized_dest_file" class="block font-bold mb-3">Destination file</label>
      <UInput id="oxidized_dest_file" v-model="settings.oxidized_dest_file" class="w-full" />
      <p class="text-muted-color mt-1">
        Oxidized
        <span class="font-mono">router.db</span>
        written by
        <span class="font-mono">factum2-oxidized</span>
        as
        <span class="font-mono">name:ip:model</span>
        per line (FQDN, primary IPv4, platform). Oxidized's CSV source must map
        <span class="font-mono">name: 0</span>, <span class="font-mono">ip: 1</span>,
        <span class="font-mono">model: 2</span>
        — the previous two-column
        <span class="font-mono">name:model</span>
        map would treat the IP as the model.
      </p>
    </div>
    <div>
      <label for="oxidized_ignore_devices" class="block font-bold mb-3">Ignore devices</label>
      <UTextarea
        id="oxidized_ignore_devices"
        v-model="settings.oxidized_ignore_devices"
        :rows="4"
        placeholder="One device name per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="oxidized_ignore_manufacturers" class="block font-bold mb-3"
        >Ignore manufacturers</label
      >
      <UTextarea
        id="oxidized_ignore_manufacturers"
        v-model="settings.oxidized_ignore_manufacturers"
        :rows="4"
        placeholder="One manufacturer per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="oxidized_ignore_models" class="block font-bold mb-3">Ignore models</label>
      <UTextarea
        id="oxidized_ignore_models"
        v-model="settings.oxidized_ignore_models"
        :rows="4"
        placeholder="One model per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="oxidized_ignore_platforms" class="block font-bold mb-3">Ignore platforms</label>
      <UTextarea
        id="oxidized_ignore_platforms"
        v-model="settings.oxidized_ignore_platforms"
        :rows="4"
        placeholder="One platform per line"
        class="w-full"
      />
    </div>
  </SettingsFormPage>
</template>
