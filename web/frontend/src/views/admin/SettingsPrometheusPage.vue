<script setup>
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="Prometheus">
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.prometheus_enabled" id="prometheus_enabled" />
      <label for="prometheus_enabled" class="font-bold">Enabled</label>
    </div>
    <p class="text-muted-color -mt-3">
      Writes a Prometheus file_sd JSON of SNMP targets for snmp_exporter. Devices with the NetBox
      custom field
      <span class="font-mono">monitor_grafana</span>
      (and a primary IPv4) are included. The Prometheus scrape job that points at snmp_exporter is
      not generated — only the target list.
    </p>
    <div>
      <label for="prometheus_dest_file" class="block font-bold mb-3">Destination file</label>
      <UInput
        id="prometheus_dest_file"
        v-model="settings.prometheus_dest_file"
        placeholder="/etc/prometheus/snmp_targets.json"
        class="w-full"
      />
    </div>
    <div>
      <label for="prometheus_reload_url" class="block font-bold mb-3">Reload URL</label>
      <UInput
        id="prometheus_reload_url"
        v-model="settings.prometheus_reload_url"
        placeholder="http://127.0.0.1:9090/-/reload"
        class="w-full"
      />
      <p class="text-muted-color mt-1">
        Optional. POSTed when the target file changes. Requires Prometheus
        <span class="font-mono">--web.enable-lifecycle</span>. Leave empty to rely on file_sd's
        refresh interval.
      </p>
    </div>
    <div>
      <label for="prometheus_module" class="block font-bold mb-3">snmp_exporter module</label>
      <UInput
        id="prometheus_module"
        v-model="settings.prometheus_module"
        placeholder="if_mib"
        class="w-full"
      />
    </div>
    <div>
      <label for="prometheus_auth" class="block font-bold mb-3">snmp_exporter auth</label>
      <UInput
        id="prometheus_auth"
        v-model="settings.prometheus_auth"
        placeholder="public_v2"
        class="w-full"
      />
      <p class="text-muted-color mt-1">
        Name of an <span class="font-mono">auths:</span> entry in snmp_exporter's
        <span class="font-mono">snmp.yml</span>, not the community string itself.
      </p>
    </div>
    <div>
      <label for="prometheus_ignore_devices" class="block font-bold mb-3">Ignore devices</label>
      <UTextarea
        id="prometheus_ignore_devices"
        v-model="settings.prometheus_ignore_devices"
        :rows="4"
        placeholder="One device name per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="prometheus_ignore_manufacturers" class="block font-bold mb-3"
        >Ignore manufacturers</label
      >
      <UTextarea
        id="prometheus_ignore_manufacturers"
        v-model="settings.prometheus_ignore_manufacturers"
        :rows="4"
        placeholder="One manufacturer per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="prometheus_ignore_models" class="block font-bold mb-3">Ignore models</label>
      <UTextarea
        id="prometheus_ignore_models"
        v-model="settings.prometheus_ignore_models"
        :rows="4"
        placeholder="One model per line"
        class="w-full"
      />
    </div>
    <div>
      <label for="prometheus_ignore_platforms" class="block font-bold mb-3">Ignore platforms</label>
      <UTextarea
        id="prometheus_ignore_platforms"
        v-model="settings.prometheus_ignore_platforms"
        :rows="4"
        placeholder="One platform per line"
        class="w-full"
      />
    </div>
  </SettingsFormPage>
</template>
