<script setup>
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="DNS">
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.dns_enabled" id="dns_enabled" />
      <label for="dns_enabled" class="font-bold">Enabled</label>
    </div>
    <div>
      <label for="dns_dest_file" class="block font-bold mb-3">Destination file</label>
      <UInput
        id="dns_dest_file"
        v-model="settings.dns_dest_file"
        placeholder="dns_dest_file"
        class="w-full"
      />
      <small class="text-muted-color"
        >worker.paths key. The JSON records path is set only in each worker's factum2-worker.yaml.
        Leave blank to use dns_dest_file.</small
      >
    </div>
    <div>
      <label for="dns_ignore_models" class="block font-bold mb-3">Ignore models</label>
      <UTextarea
        id="dns_ignore_models"
        v-model="settings.dns_ignore_models"
        :rows="4"
        placeholder="One model per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="dns_ignore_platforms" class="block font-bold mb-3">Ignore platforms</label>
      <UTextarea
        id="dns_ignore_platforms"
        v-model="settings.dns_ignore_platforms"
        :rows="4"
        placeholder="One platform per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="dns_zones_file" class="block font-bold mb-3">Include file</label>
      <UInput
        id="dns_zones_file"
        v-model="settings.dns_zones_file"
        placeholder="dns_zones_file"
        class="w-full"
      />
      <small class="text-muted-color"
        >worker.paths key, for example dns_zones_file. factum2-dns writes the YAML zone list at the
        path that key names on the worker when the zone editor is on. Add that path as its own item
        in the administrator-managed <code>/etc/dnsmgr2/dnsmgr2.yaml</code>:
        <code>- include: /etc/dnsmgr2/factum2-zones.yaml</code>
        (after the <code>host_dns_template</code> entry). Leave blank to skip writing when the zone
        editor is off.</small
      >
    </div>
  </SettingsFormPage>
</template>
