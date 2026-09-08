<script setup>
import { useId } from 'vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'IpamPrefixForm' })

const form = defineModel({ type: Object, required: true })
defineProps({
  prefixDisabled: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  autofocusPrefix: { type: Boolean, default: false },
  autofocusDescription: { type: Boolean, default: false },
  showVrf: { type: Boolean, default: false },
  vrfName: { type: String, default: '' },
})

const authStore = useAuthStore()
const uid = useId()

function defaultGatewayHint(prefix) {
  const s = (prefix || '').trim()
  const slash = s.lastIndexOf('/')
  if (slash < 0) return ''
  const addr = s.slice(0, slash)
  const bits = Number(s.slice(slash + 1))
  if (!Number.isInteger(bits)) return ''
  if (addr.includes('.')) {
    const parts = addr.split('.').map((p) => Number(p))
    if (parts.length !== 4 || parts.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) {
      return ''
    }
    if (bits > 30) return addr
    const mask = bits === 0 ? 0 : (~0 << (32 - bits)) >>> 0
    let n = ((parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]) >>> 0
    n = (n & mask) + 1
    return [(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.')
  }
  if (bits <= 126 && addr.includes(':')) {
    if (addr.endsWith('::')) return `${addr}1`
    if (addr.endsWith('::0')) return addr.slice(0, -1) + '1'
  }
  return ''
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <label class="block font-bold mb-2">Prefix</label>
      <UInput
        v-model="form.prefix"
        placeholder="10.0.1.0/24"
        class="w-full"
        :disabled="disabled || prefixDisabled"
        :autofocus="autofocusPrefix"
      />
    </div>
    <div v-if="showVrf">
      <label class="block font-bold mb-2">VRF</label>
      <UInput :model-value="vrfName || '—'" disabled class="w-full" />
    </div>
    <div>
      <label class="block font-bold mb-2">Description</label>
      <UInput
        v-model="form.description"
        class="w-full"
        :disabled="disabled"
        :autofocus="autofocusDescription"
      />
    </div>
    <template v-if="authStore.dhcpEnabled">
      <div class="flex items-center gap-2">
        <USwitch v-model="form.dhcp_enabled" :id="uid + '-dhcp'" :disabled="disabled" />
        <label :for="uid + '-dhcp'" class="font-bold">DHCP server</label>
      </div>
      <template v-if="form.dhcp_enabled">
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block font-bold mb-2">Range start</label>
            <UInput
              v-model="form.dhcp_range_start"
              placeholder="192.0.2.100"
              class="w-full"
              :disabled="disabled"
            />
          </div>
          <div>
            <label class="block font-bold mb-2">Range end</label>
            <UInput
              v-model="form.dhcp_range_end"
              placeholder="192.0.2.200"
              class="w-full"
              :disabled="disabled"
            />
          </div>
        </div>
        <small class="text-muted-color -mt-2"
          >Must sit inside the prefix. Leave empty for reservations only.</small
        >
        <div>
          <label class="block font-bold mb-2">Default gateway</label>
          <UInput
            v-model="form.dhcp_gateway"
            :placeholder="defaultGatewayHint(form.prefix) || 'first address in prefix'"
            class="w-full"
            :disabled="disabled"
          />
          <small class="block text-muted-color mt-1"
            >Empty uses the first usable address in the prefix.</small
          >
        </div>
        <div>
          <label class="block font-bold mb-2">DNS servers</label>
          <UTextarea
            v-model="form.dhcp_dns_servers"
            :rows="2"
            placeholder="Leave empty to use the default"
            class="w-full"
            :disabled="disabled"
          />
          <small class="text-muted-color"
            >One IP per line. Empty uses Destinations → DHCP → Default DNS servers.</small
          >
        </div>
      </template>
    </template>
  </div>
</template>
