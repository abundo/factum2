<script setup>
import DestinationFilterFields from '@/components/DestinationFilterFields.vue'
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="Common">
    <p class="text-muted-color -mt-2">
      Default lists for DNS, Icinga, LibreNMS, Oxidized, and Prometheus. A destination uses its own
      list when that list has an entry, and this list when its own is empty. Ignore devices, models,
      platforms, and manufacturers are one name per line. Ignore interfaces is one regular expression
      per line, matched against the whole interface name. DNS omits that interface's records. Icinga
      omits its interface check. Prometheus drops matching samples. LibreNMS turns alerting off.
      Roles enabled is one regular expression per line. DNS, Icinga, Oxidized, and Prometheus include
      a device when its role matches, and include every role when the list is empty. LibreNMS turns
      alerting on for an interface whose role matches.
    </p>
    <DestinationFilterFields :settings="settings" prefix="common" />
  </SettingsFormPage>
</template>
