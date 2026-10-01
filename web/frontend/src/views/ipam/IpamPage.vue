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
} from '@/api/ipam'
import IpamPrefixForm from '@/components/IpamPrefixForm.vue'
import IpamPrefixTree from '@/components/IpamPrefixTree.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'IpamPage' })

const toast = useToast()
const authStore = useAuthStore()
const { confirmDelete } = useConfirm()
const treeRef = ref(null)
const filter = ref('')
const saving = ref(false)
const selected = ref(null)

const menu = ref({ open: false, x: 0, y: 0, node: null })
const dialog = ref(null)
const form = ref({})

const menuItems = computed(() => itemsFor(menu.value.node))

const canDeleteSelected = computed(() => {
  const node = selected.value
  if (!node || !authStore.canWrite) return false
  const kind = node.kind || node.type
  if (kind === 'namespace' || kind === 'allocated') return true
  if (kind === 'vrf') return node.source !== 'netbox' && !node.is_default
  return false
})

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function applyFilter(q) {
  treeRef.value?.filter(q)
}

function itemsFor(node) {
  const write = authStore.canWrite
  if (!node) {
    return write
      ? [
          { id: 'add-prefix', label: 'Add prefix' },
          { id: 'add-vrf', label: 'Add VRF' },
          { id: 'add-ns', label: 'Add namespace' },
        ]
      : []
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
      items.push({ id: 'add-prefix', label: 'Add prefix' }, { id: 'add-vrf', label: 'Add VRF' })
      break
    case 'vrf':
      items.push({ id: 'add-prefix', label: 'Add prefix' })
      break
    case 'allocated':
      items.push(
        { id: 'add-prefix', label: 'Add child prefix' },
        { id: 'edit-prefix', label: authStore.dhcpEnabled ? 'Edit prefix' : 'Edit description' },
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

function onSelect(node) {
  selected.value = node
}

function openEditPrefix(node) {
  if (!node) return
  const kind = node.kind || node.type
  if (kind !== 'allocated') return
  treeRef.value?.selectKey(node.key)
}

function closeMenu() {
  menu.value = { ...menu.value, open: false }
}

async function runMenu(id, node = menu.value.node) {
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
  if (id === 'add-vrf') {
    form.value = {
      namespace_id: node?.namespace_id || 0,
      name: '',
      description: '',
      rd: '',
      import_rt: '',
      export_rt: '',
    }
    dialog.value = 'vrf'
    return
  }
  if (id === 'add-prefix') {
    const kind = node?.kind || node?.type
    let vrfId = 0
    if (kind === 'vrf') vrfId = node.vrf_id || node.id
    else if (kind === 'allocated') vrfId = node.vrf_id || 0
    form.value = {
      namespace_id: node?.namespace_id || 0,
      vrf_id: vrfId,
      prefix: '',
      description: '',
      parent_key: node?.key,
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
    openEditPrefix(node)
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
    req = createNamespace(payload)
  } else if (dialog.value === 'vrf') {
    const payload = {
      name: (f.name ?? '').trim(),
      description: f.description ?? '',
      rd: (f.rd ?? '').trim(),
      import_rt: (f.import_rt ?? '').trim(),
      export_rt: (f.export_rt ?? '').trim(),
    }
    if (!payload.name) {
      saving.value = false
      return
    }
    req = createVrf(f.namespace_id, payload)
  } else if (dialog.value === 'prefix') {
    const payload = prefixPayload(f)
    if (!(f.prefix ?? '').trim()) {
      saving.value = false
      return
    }
    req = createPrefix(f.namespace_id, payload)
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

async function deleteSelected() {
  const node = selected.value
  if (!canDeleteSelected.value || !node) return
  const kind = node.kind || node.type
  let what = `prefix ${node.title}`
  let detail = ''
  const target = { kind: 'prefix', id: node.prefix_id || node.id, namespace_id: node.namespace_id }
  if (kind === 'namespace') {
    what = `namespace ${node.title}`
    target.kind = 'ns'
    target.id = node.id
  } else if (kind === 'vrf') {
    what = `VRF ${node.title}`
    detail = 'This cannot be undone.'
    target.kind = 'vrf'
    target.id = node.vrf_id || node.id
  }
  if (!(await confirmDelete(what, detail))) return
  performDelete(target)
}

function performDelete(c) {
  if (!c) return
  saving.value = true
  let req
  if (c.kind === 'ns') req = deleteNamespace(c.id)
  if (c.kind === 'vrf') req = deleteVrf(c.namespace_id, c.id)
  if (c.kind === 'prefix') req = deletePrefix(c.namespace_id, c.id)
  req
    .then(() => {
      selected.value = null
      treeRef.value?.reload()
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
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
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
          label="Prefix"
          @click="runMenu('add-prefix', null)"
        />
        <UButton
          v-if="authStore.canWrite"
          icon="i-lucide-plus"
          size="sm"
          label="VRF"
          @click="runMenu('add-vrf', null)"
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
      Right-click empty space to add a prefix or VRF at the root (no namespace needed). Right-click a
      namespace for prefixes and extra VRFs in that space. VRF names are unique across all
      namespaces. Select a namespace, VRF, or prefix and use Delete in its details. A default VRF, a NetBox VRF,
      and a VRF that still has prefixes cannot be removed. Prefixes under a VRF cannot overlap the
      root or any other VRF. VRFs synced from NetBox appear at the root and are read-only. Click a
      row to see details. Click [+] / [−] to expand or collapse.
    </p>
    <IpamPrefixTree
      ref="treeRef"
      class="min-h-0 flex-1"
      @contextmenu="onContextMenu"
      :allow-delete="canDeleteSelected"
      @select="onSelect"
      @delete="deleteSelected"
    />
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

  <FormModal
    :open="dialog === 'ns'"
    :source="form"
    title="Namespace"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Description">
          <UInput v-model="form.description" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton class="ms-auto" label="Cancel" variant="ghost" @click="close" />
        <UButton label="Save" :loading="saving" @click="saveDialog" />
      </div>
    </template>
  </FormModal>

  <FormModal
    :open="dialog === 'vrf'"
    :source="form"
    title="VRF"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Description">
          <UInput v-model="form.description" class="w-full" />
        </UFormField>
        <UFormField label="RD">
          <UInput v-model="form.rd" placeholder="65000:1" class="w-full" />
        </UFormField>
        <UFormField label="Import route-target">
          <UInput v-model="form.import_rt" placeholder="65000:1" class="w-full" />
        </UFormField>
        <UFormField label="Export route-target">
          <UInput v-model="form.export_rt" placeholder="65000:1" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton class="ms-auto" label="Cancel" variant="ghost" @click="close" />
        <UButton label="Save" :loading="saving" @click="saveDialog" />
      </div>
    </template>
  </FormModal>

  <FormModal
    :open="dialog === 'prefix'"
    :source="form"
    title="Add prefix"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <IpamPrefixForm v-model="form" autofocus-prefix />
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton class="ms-auto" label="Cancel" variant="ghost" @click="close" />
        <UButton label="Add" :loading="saving" @click="saveDialog" />
      </div>
    </template>
  </FormModal>
</template>
