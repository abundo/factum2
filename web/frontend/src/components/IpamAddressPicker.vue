<script setup>
import { computed, ref, watch } from 'vue'
import { getForest, getPrefixHosts, listVrfs } from '@/api/ipam'
import IpamPickerTree from '@/components/IpamPickerTree.vue'
import SearchInput from '@/components/SearchInput.vue'

defineOptions({ name: 'IpamAddressPicker' })

const open = defineModel('open', { type: Boolean })
const emit = defineEmits(['select'])

const vrfQuery = ref('')
const vrfs = ref([])
const vrfsLoading = ref(false)
const selectedVrfId = ref(null)

const prefixes = ref([])
const prefixesLoading = ref(false)
const selectedPrefix = ref(null)
const expanded = ref({})

const hosts = ref(null)
const hostsLoading = ref(false)
const hostPage = ref(0)
const selectedCIDR = ref('')

const filteredVrfs = computed(() => {
  const q = vrfQuery.value.trim().toLowerCase()
  if (!q) return vrfs.value
  return vrfs.value.filter((v) => {
    const name = (v.name || '').toLowerCase()
    const ns = (v.namespace_name || '').toLowerCase()
    const desc = (v.description || '').toLowerCase()
    return name.includes(q) || ns.includes(q) || desc.includes(q)
  })
})

function vrfLabel(v) {
  if (!v) return ''
  const ns = v.namespace_name ? `${v.namespace_name} / ` : ''
  return `${ns}${v.name}`
}

function loadVrfs() {
  vrfsLoading.value = true
  listVrfs()
    .then((rows) => {
      vrfs.value = rows ?? []
    })
    .catch(() => {
      vrfs.value = []
    })
    .finally(() => {
      vrfsLoading.value = false
    })
}

function selectVrf(v) {
  selectedVrfId.value = v.id
  selectedPrefix.value = null
  hosts.value = null
  selectedCIDR.value = ''
  expanded.value = {}
  prefixesLoading.value = true
  getForest(`vrf:${v.id}`)
    .then((rows) => {
      prefixes.value = (rows ?? []).map(toNode)
    })
    .catch(() => {
      prefixes.value = []
    })
    .finally(() => {
      prefixesLoading.value = false
    })
}

function toNode(n) {
  return {
    key: n.key,
    title: n.title,
    lazy: !!n.lazy,
    children: Array.isArray(n.children) ? n.children.map(toNode) : [],
    loaded: Array.isArray(n.children) && n.children.length > 0,
    ...n.data,
  }
}

function isAllocatedKind(node) {
  return (node.kind || node.type) === 'allocated'
}

async function toggleExpand(node) {
  if (!node.lazy && !(node.children && node.children.length)) return
  const next = !expanded.value[node.key]
  expanded.value = { ...expanded.value, [node.key]: next }
  if (!next || node.loaded || (node.children && node.children.length)) return
  const rows = await getForest(node.key)
  node.children = (rows ?? []).map(toNode)
  node.loaded = true
}

function clickPrefix(node) {
  if (!isAllocatedKind(node)) {
    toggleExpand(node)
    return
  }
  selectedPrefix.value = node
  selectedCIDR.value = ''
  hostPage.value = 0
  loadHosts()
}

function loadHosts() {
  const node = selectedPrefix.value
  const id = node?.prefix_id || node?.id
  if (!id) return
  hostsLoading.value = true
  getPrefixHosts(id, hostPage.value)
    .then((data) => {
      hosts.value = data
      hostPage.value = data.page ?? 0
    })
    .catch(() => {
      hosts.value = null
    })
    .finally(() => {
      hostsLoading.value = false
    })
}

function prevPage() {
  if (!hosts.value || hosts.value.page <= 0) return
  hostPage.value = hosts.value.page - 1
  loadHosts()
}

function nextPage() {
  if (!hosts.value || hosts.value.page + 1 >= hosts.value.page_count) return
  hostPage.value = hosts.value.page + 1
  loadHosts()
}

function hostClass(cell) {
  if (cell.allocated) return 'bg-red-600 text-white cursor-not-allowed'
  if (selectedCIDR.value === cell.cidr) return 'bg-green-700 text-white ring-2 ring-white'
  return 'bg-green-600 text-white hover:bg-green-500 cursor-pointer'
}

function pickHost(cell) {
  if (cell.allocated) return
  selectedCIDR.value = cell.cidr
  const vrf = vrfs.value.find((v) => v.id === selectedVrfId.value)
  emit('select', {
    address: cell.cidr,
    vrf: hosts.value?.vrf_name || (vrf && !vrf.is_default ? vrf.name : '') || '',
    prefix_id: hosts.value?.prefix_id,
    prefix: hosts.value?.prefix,
  })
  open.value = false
}

function lastOctet(addr) {
  if (!addr) return ''
  if (addr.includes('.')) {
    const parts = addr.split('.')
    return parts[parts.length - 1]
  }
  const parts = addr.split(':')
  return parts[parts.length - 1] || '0'
}

watch(open, (isOpen) => {
  if (!isOpen) return
  vrfQuery.value = ''
  selectedVrfId.value = null
  prefixes.value = []
  selectedPrefix.value = null
  hosts.value = null
  selectedCIDR.value = ''
  loadVrfs()
})
</script>

<template>
  <UModal
    v-model:open="open"
    title="Pick IP address"
    :ui="{ content: 'sm:max-w-6xl w-[95vw]' }"
  >
    <template #body>
      <div class="grid grid-cols-1 md:grid-cols-3 gap-3 min-h-[28rem]">
        <div class="flex min-h-0 flex-col gap-2 border border-default rounded-md p-2">
          <div class="font-bold text-sm">VRFs</div>
          <SearchInput v-model="vrfQuery" placeholder="Search VRFs…" />
          <div class="min-h-0 flex-1 overflow-auto">
            <div v-if="vrfsLoading" class="text-sm text-muted-color">Loading…</div>
            <div v-else-if="!filteredVrfs.length" class="text-sm text-muted-color">No VRFs.</div>
            <button
              v-for="v in filteredVrfs"
              :key="v.id"
              type="button"
              class="block w-full text-left rounded px-2 py-1.5 text-sm"
              :class="
                selectedVrfId === v.id ? 'bg-primary/20 font-medium' : 'hover:bg-elevated'
              "
              @click="selectVrf(v)"
            >
              <div>{{ vrfLabel(v) }}</div>
              <div v-if="v.description" class="text-xs text-muted-color truncate">
                {{ v.description }}
              </div>
            </button>
          </div>
        </div>

        <div class="flex min-h-0 flex-col gap-2 border border-default rounded-md p-2">
          <div class="font-bold text-sm">Prefixes</div>
          <div class="min-h-0 flex-1 overflow-auto font-mono text-sm">
            <div v-if="!selectedVrfId" class="text-muted-color font-sans">Select a VRF.</div>
            <div v-else-if="prefixesLoading" class="text-muted-color font-sans">Loading…</div>
            <div v-else-if="!prefixes.length" class="text-muted-color font-sans">
              No prefixes in this VRF.
            </div>
            <IpamPickerTree
              v-else
              :nodes="prefixes"
              :expanded="expanded"
              :selected-key="selectedPrefix?.key"
              @toggle="toggleExpand"
              @select="clickPrefix"
            />
          </div>
        </div>

        <div class="flex min-h-0 flex-col gap-2 border border-default rounded-md p-2">
          <div class="flex items-center justify-between gap-2">
            <div class="font-bold text-sm truncate">
              {{ hosts?.page_prefix || selectedPrefix?.title || 'Addresses' }}
            </div>
            <div v-if="hosts && hosts.page_count > 1" class="flex items-center gap-1 shrink-0">
              <UButton
                size="xs"
                color="neutral"
                variant="outline"
                icon="i-lucide-chevron-left"
                :disabled="hosts.page <= 0"
                @click="prevPage"
              />
              <span class="text-xs text-muted-color">
                {{ hosts.page + 1 }}/{{ hosts.page_count }}
              </span>
              <UButton
                size="xs"
                color="neutral"
                variant="outline"
                icon="i-lucide-chevron-right"
                :disabled="hosts.page + 1 >= hosts.page_count"
                @click="nextPage"
              />
            </div>
          </div>
          <div class="min-h-0 flex-1 overflow-auto">
            <div v-if="!selectedPrefix" class="text-sm text-muted-color">Select a prefix.</div>
            <div v-else-if="hostsLoading" class="text-sm text-muted-color">Loading…</div>
            <div
              v-else-if="hosts?.hosts?.length"
              class="grid gap-px"
              style="grid-template-columns: repeat(16, minmax(0, 1fr))"
            >
              <button
                v-for="cell in hosts.hosts"
                :key="cell.address"
                type="button"
                class="aspect-square text-[10px] leading-none rounded-sm flex items-center justify-center"
                :class="hostClass(cell)"
                :title="cell.cidr + (cell.allocated ? ' (allocated)' : ' (free)')"
                :disabled="cell.allocated"
                @click="pickHost(cell)"
              >
                {{ lastOctet(cell.address) }}
              </button>
            </div>
            <div v-else class="text-sm text-muted-color">No addresses to show.</div>
          </div>
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="open = false" />
    </template>
  </UModal>
</template>
