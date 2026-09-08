<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  createNamespace,
  createPrefix,
  createVrf,
  deleteNamespace,
  deletePrefix,
  deleteVrf,
  updateNamespace,
  updatePrefix,
  updateVrf,
} from '@/api/ipam'
import IpamPrefixTree from '@/components/IpamPrefixTree.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'IpamPage' })

const toast = useToast()
const authStore = useAuthStore()
const treeRef = ref(null)
const filter = ref('')
const saving = ref(false)

const menu = ref({ open: false, x: 0, y: 0, node: null })
const dialog = ref(null)
const form = ref({})
const confirm = ref(null)

const menuItems = computed(() => itemsFor(menu.value.node))

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function applyFilter(q) {
  treeRef.value?.filter(q)
}

function itemsFor(node) {
  const write = authStore.canWrite
  if (!node) {
    return write ? [{ id: 'add-ns', label: 'Add namespace' }] : []
  }
  const kind = node.kind || node.type
  const items = [
    { id: 'expand', label: 'Expand' },
    { id: 'collapse', label: 'Collapse' },
  ]
  if (!write) return items
  items.push({ id: 'sep' })
  switch (kind) {
    case 'namespace':
      items.push(
        { id: 'add-prefix', label: 'Add prefix' },
        { id: 'add-vrf', label: 'Add VRF' },
        { id: 'rename-ns', label: 'Rename' },
        { id: 'sep2' },
        { id: 'del-ns', label: 'Delete namespace', danger: true },
      )
      break
    case 'vrf':
      items.push(
        { id: 'add-prefix', label: 'Add prefix' },
        { id: 'rename-vrf', label: 'Rename' },
        { id: 'sep2' },
        { id: 'del-vrf', label: 'Delete VRF', danger: true },
      )
      break
    case 'allocated':
      items.push(
        { id: 'add-prefix', label: 'Add child prefix' },
        { id: 'edit-prefix', label: authStore.dhcpEnabled ? 'Edit prefix' : 'Edit description' },
        { id: 'sep2' },
        { id: 'del-prefix', label: 'Delete', danger: true },
      )
      break
    default:
      break
  }
  return items
}

function onContextMenu({ x, y, node }) {
  menu.value = { open: true, x, y, node }
}

function closeMenu() {
  menu.value = { ...menu.value, open: false }
}

async function runMenu(id) {
  const node = menu.value.node
  closeMenu()
  if (id === 'expand') {
    treeRef.value?.expandNode(node?.key)
    return
  }
  if (id === 'collapse') {
    treeRef.value?.collapseNode(node?.key)
    return
  }
  if (id === 'add-ns') {
    form.value = { name: '', description: '' }
    dialog.value = 'ns'
    return
  }
  if (id === 'rename-ns') {
    form.value = { id: node.id, name: node.title, description: node.description ?? '' }
    dialog.value = 'ns'
    return
  }
  if (id === 'add-vrf') {
    form.value = { namespace_id: node.namespace_id, name: '', description: '' }
    dialog.value = 'vrf'
    return
  }
  if (id === 'rename-vrf') {
    form.value = {
      namespace_id: node.namespace_id,
      id: node.id,
      name: node.title,
      description: node.description ?? '',
    }
    dialog.value = 'vrf'
    return
  }
  if (id === 'add-prefix') {
    const kind = node.kind || node.type
    let vrfId = 0
    if (kind === 'vrf') vrfId = node.vrf_id || node.id
    else if (kind === 'allocated') vrfId = node.vrf_id || 0
    form.value = {
      namespace_id: node.namespace_id,
      vrf_id: vrfId,
      prefix: '',
      description: '',
      parent_key: node.key,
      dhcp_enabled: false,
      dhcp_range_start: '',
      dhcp_range_end: '',
      dhcp_gateway: '',
      dhcp_dns_servers: '',
    }
    dialog.value = 'prefix'
    return
  }
  if (id === 'edit-prefix') {
    form.value = {
      id: node.prefix_id || node.id,
      namespace_id: node.namespace_id,
      prefix: node.title,
      description: node.description ?? '',
      parent_key: node.key,
      dhcp_enabled: !!node.dhcp_enabled,
      dhcp_range_start: node.dhcp_range_start ?? '',
      dhcp_range_end: node.dhcp_range_end ?? '',
      dhcp_gateway: node.dhcp_gateway ?? '',
      dhcp_dns_servers: node.dhcp_dns_servers ?? '',
    }
    dialog.value = 'prefix'
    return
  }
  if (id === 'del-ns') {
    confirm.value = { kind: 'ns', id: node.id, namespace_id: node.namespace_id, label: node.title }
    return
  }
  if (id === 'del-vrf') {
    if (node.is_default) {
      toast.add({
        color: 'warning',
        title: 'Cannot delete default VRF',
        description:
          'Rename it if you want another name. Extra VRFs can be deleted once they have no prefixes.',
      })
      return
    }
    confirm.value = {
      kind: 'vrf',
      id: node.vrf_id || node.id,
      namespace_id: node.namespace_id,
      label: node.title,
    }
    return
  }
  if (id === 'del-prefix') {
    confirm.value = {
      kind: 'prefix',
      id: node.prefix_id || node.id,
      namespace_id: node.namespace_id,
      label: node.title,
    }
  }
}

function prefixPayload(f) {
  const payload = {
    prefix: (f.prefix ?? '').trim(),
    vrf_id: Number(f.vrf_id) || 0,
    description: f.description ?? '',
  }
  if (authStore.dhcpEnabled) {
    payload.dhcp_enabled = !!f.dhcp_enabled
    payload.dhcp_range_start = (f.dhcp_range_start ?? '').trim()
    payload.dhcp_range_end = (f.dhcp_range_end ?? '').trim()
    payload.dhcp_gateway = (f.dhcp_gateway ?? '').trim()
    payload.dhcp_dns_servers = f.dhcp_dns_servers ?? ''
  }
  return payload
}

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

function saveDialog() {
  const f = form.value
  saving.value = true
  let req
  if (dialog.value === 'ns') {
    const payload = { name: (f.name ?? '').trim(), description: f.description ?? '' }
    if (!payload.name) {
      saving.value = false
      return
    }
    req = f.id ? updateNamespace(f.id, payload) : createNamespace(payload)
  } else if (dialog.value === 'vrf') {
    const payload = { name: (f.name ?? '').trim(), description: f.description ?? '' }
    if (!payload.name) {
      saving.value = false
      return
    }
    req = f.id ? updateVrf(f.namespace_id, f.id, payload) : createVrf(f.namespace_id, payload)
  } else if (dialog.value === 'prefix') {
    const payload = prefixPayload(f)
    if (f.id) {
      req = updatePrefix(f.namespace_id, f.id, payload)
    } else {
      if (!(f.prefix ?? '').trim()) {
        saving.value = false
        return
      }
      req = createPrefix(f.namespace_id, payload)
    }
  }
  if (!req) {
    saving.value = false
    return
  }
  const revealKeys =
    dialog.value === 'prefix' && f.parent_key ? treeRef.value?.keyPath(f.parent_key) : []
  req
    .then(() => {
      dialog.value = null
      return treeRef.value?.reload(revealKeys)
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Request failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function performDelete() {
  const c = confirm.value
  if (!c) return
  saving.value = true
  let req
  if (c.kind === 'ns') req = deleteNamespace(c.id)
  if (c.kind === 'vrf') req = deleteVrf(c.namespace_id, c.id)
  if (c.kind === 'prefix') req = deletePrefix(c.namespace_id, c.id)
  req
    .then(() => {
      confirm.value = null
      if (c.kind === 'ns') treeRef.value?.reload()
      else {
        // Parent is one level up; full reload is safest after delete.
        treeRef.value?.reload()
      }
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Delete failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function onDocClick() {
  if (menu.value.open) closeMenu()
}

onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-3 shrink-0">
      <h4 class="m-0">IPAM</h4>
      <div class="flex flex-wrap gap-2 items-center">
        <SearchInput
          v-model="filter"
          placeholder="Filter..."
          class="w-56"
          @update:model-value="applyFilter"
        />
        <UButton
          icon="i-lucide-unfold-vertical"
          variant="outline"
          color="neutral"
          size="sm"
          title="Expand all"
          @click="treeRef?.expandAll()"
        />
        <UButton
          icon="i-lucide-fold-vertical"
          variant="outline"
          color="neutral"
          size="sm"
          title="Collapse all"
          @click="treeRef?.collapseAll()"
        />
        <UButton
          v-if="authStore.canWrite"
          icon="i-lucide-plus"
          size="sm"
          label="Namespace"
          @click="runMenu('add-ns')"
        />
      </div>
    </div>
    <p class="text-muted-color text-sm mb-3 shrink-0">
      Right-click a namespace to add a prefix (or an extra VRF). Prefixes under a VRF cannot overlap
      the namespace root or any other VRF. Click [+] / [−] to expand or collapse.
    </p>
    <IpamPrefixTree ref="treeRef" class="min-h-0 flex-1" @contextmenu="onContextMenu" />
  </div>

  <div
    v-if="menu.open && menuItems.length"
    class="ipam-context-menu"
    :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
    @click.stop
  >
    <template v-for="(item, i) in menuItems" :key="i">
      <hr v-if="item.id.startsWith('sep')" />
      <button v-else :class="{ danger: item.danger }" type="button" @click="runMenu(item.id)">
        {{ item.label }}
      </button>
    </template>
  </div>

  <UModal :open="dialog === 'ns'" title="Namespace" @update:open="(v) => !v && (dialog = null)">
    <template #body>
      <div class="flex flex-col gap-4">
        <div>
          <label class="block font-bold mb-2">Name</label>
          <UInput v-model="form.name" autofocus />
        </div>
        <div>
          <label class="block font-bold mb-2">Description</label>
          <UInput v-model="form.description" />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal :open="dialog === 'vrf'" title="VRF" @update:open="(v) => !v && (dialog = null)">
    <template #body>
      <div class="flex flex-col gap-4">
        <div>
          <label class="block font-bold mb-2">Name</label>
          <UInput v-model="form.name" autofocus />
        </div>
        <div>
          <label class="block font-bold mb-2">Description</label>
          <UInput v-model="form.description" />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'prefix'"
    :title="form.id ? 'Edit prefix' : 'Add prefix'"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <div>
          <label class="block font-bold mb-2">Prefix</label>
          <UInput
            v-model="form.prefix"
            placeholder="10.0.1.0/24"
            :disabled="!!form.id"
            :autofocus="!form.id"
          />
        </div>
        <div>
          <label class="block font-bold mb-2">Description</label>
          <UInput v-model="form.description" :autofocus="!!form.id" />
        </div>
        <template v-if="authStore.dhcpEnabled">
          <div class="flex items-center gap-2">
            <USwitch v-model="form.dhcp_enabled" id="prefix_dhcp_enabled" />
            <label for="prefix_dhcp_enabled" class="font-bold">DHCP server</label>
          </div>
          <template v-if="form.dhcp_enabled">
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block font-bold mb-2">Range start</label>
                <UInput v-model="form.dhcp_range_start" placeholder="192.0.2.100" />
              </div>
              <div>
                <label class="block font-bold mb-2">Range end</label>
                <UInput v-model="form.dhcp_range_end" placeholder="192.0.2.200" />
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
              />
              <small class="text-muted-color"
                >One IP per line. Empty uses Settings → Factum → Default DNS servers.</small
              >
            </div>
          </template>
        </template>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton :label="form.id ? 'Save' : 'Add'" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal :open="!!confirm" title="Delete" @update:open="(v) => !v && (confirm = null)">
    <template #body>
      Delete <strong>{{ confirm?.label }}</strong
      >?
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="confirm = null" />
      <UButton label="Delete" color="error" :loading="saving" @click="performDelete" />
    </template>
  </UModal>
</template>
