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
      <UInput id="dns_dest_file" v-model="settings.dns_dest_file" class="w-full" />
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
        placeholder="/etc/dnsmgr2/zones.yaml"
        class="w-full"
      />
      <small class="text-muted-color"
        >YAML zone list written by factum2-dns when the zone editor or DHCP is on. Add it as its own
        item in the administrator-managed <code>/etc/dnsmgr2/dnsmgr2.yaml</code>:
        <code>- include: /etc/dnsmgr2/zones.yaml</code>
        (after the <code>host_dns_template</code> entry). Leave blank to skip writing.</small
      >
    </div>
  </SettingsFormPage>
</template>
