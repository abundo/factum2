<script setup>
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="DHCP">
    <div class="flex items-center gap-2">
      <USwitch
        :model-value="!!settings.dhcp_enabled"
        id="dhcp_enabled"
        @update:model-value="settings.dhcp_enabled = $event"
      />
      <label for="dhcp_enabled" class="font-bold">Enabled</label>
    </div>
    <small class="text-muted-color -mt-4"
      >Per-prefix DHCP in IPAM (range, gateway, DNS servers) and a MAC column on DNS zone records
      for static reservations. Off by default. Turning this off hides the UI; it does not delete
      stored DHCP data. Kea paths and <code>host_dhcp_template</code> live in the
      administrator-managed <code>dnsmgr2.yaml</code>.</small
    >
    <div v-if="settings.dhcp_enabled">
      <label for="dhcp_dns_servers" class="block font-bold mb-3">Default DNS servers</label>
      <UTextarea
        id="dhcp_dns_servers"
        v-model="settings.dhcp_dns_servers"
        :rows="3"
        placeholder="One IP address per line"
        class="w-full"
      />
      <small class="text-muted-color"
        >Offered to DHCP clients unless a prefix overrides them. Domain name is the default domain
        on Settings → Factum.</small
      >
    </div>
    <div>
      <label for="dhcp_prefixes_file" class="block font-bold mb-3">Include file</label>
      <UInput
        id="dhcp_prefixes_file"
        v-model="settings.dhcp_prefixes_file"
        placeholder="/etc/dnsmgr2/prefixes.yaml"
        class="w-full"
      />
      <small class="text-muted-color"
        >YAML prefix list written by factum2-dns when DHCP is on. Add it as its own item in the
        administrator-managed <code>/etc/dnsmgr2/dnsmgr2.yaml</code>:
        <code>- include: /etc/dnsmgr2/prefixes.yaml</code>
        (after the <code>host_dhcp_template</code> entry). Leave blank to skip writing.</small
      >
    </div>
  </SettingsFormPage>
</template>
