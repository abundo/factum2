<script setup>
import { computed, markRaw, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { VueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import { getConnectionGraph, getConnectionLayout, saveConnectionLayout } from '@/api/connections'
import { getDevices } from '@/api/devices'
import { getRacks } from '@/api/racks'
import { getSites } from '@/api/sites'
import ConnectionDeviceNode from '@/components/dcim/ConnectionDeviceNode.vue'
import DevicePairCables from '@/components/dcim/DevicePairCables.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'ConnectionsPage' })

const route = useRoute()
const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()
const canWrite = computed(() => authStore.canWrite)

const flowRef = ref(null)

const loading = ref(true)
const error = ref(null)
const graph = ref(null)
const layout = ref({ revision: 1, nodes: {} })
const nodes = ref([])
const edges = ref([])
const selectedEdge = ref(null)
const sites = ref([])
const racks = ref([])
const devices = ref([])
const filters = ref({
  site_id: route.query.site_id ? Number(route.query.site_id) : undefined,
  rack_id: route.query.rack_id ? Number(route.query.rack_id) : undefined,
  device_id: route.query.device_id ? Number(route.query.device_id) : undefined,
  depth: 1,
})

function defaultView() {
  if (route.query.view === 'graph' || route.query.site_id || route.query.rack_id) return 'graph'
  return 'pair'
}

const view = ref(defaultView())
const deviceAId = ref(route.query.a ? Number(route.query.a) : undefined)
const deviceBId = ref(route.query.b ? Number(route.query.b) : undefined)

const viewItems = [
  { label: 'Between devices', value: 'pair' },
  { label: 'Graph', value: 'graph' },
]

const deviceItems = computed(() =>
  devices.value.map((d) => ({
    id: d.id,
    name: d.site ? `${d.name} (${d.site})` : d.name,
  })),
)

const nodeTypes = { device: markRaw(ConnectionDeviceNode) }

const siteItems = computed(() => [
  { label: 'Any site', value: undefined },
  ...sites.value.map((s) => ({ label: s.name, value: s.id })),
])
const rackItems = computed(() => [
  { label: 'Any rack', value: undefined },
  ...racks.value.map((r) => ({ label: r.name, value: r.id })),
])

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function params() {
  const p = { depth: filters.value.depth || 1 }
  if (filters.value.site_id) p.site_id = filters.value.site_id
  if (filters.value.rack_id) p.rack_id = filters.value.rack_id
  if (filters.value.device_id) p.device_id = filters.value.device_id
  return p
}

function applyGraph(g, positions) {
  graph.value = g
  const pos = positions || {}
  nodes.value = (g.nodes || []).map((n, i) => ({
    id: String(n.id),
    type: 'device',
    position: pos[String(n.id)] || { x: (i % 8) * 220, y: Math.floor(i / 8) * 160 },
    data: {
      label: n.name,
      external: n.external,
      interfaces: n.interfaces || [],
    },
    draggable: true,
  }))
  edges.value = (g.edges || []).map((e) => ({
    id: String(e.id),
    source: String(e.device_a_id),
    target: String(e.device_b_id),
    sourceHandle: String(e.interface_a_id),
    targetHandle: `in-${e.interface_b_id}`,
    label: e.label || '',
    selectable: true,
    updatable: false,
    data: e,
  }))
}

function load() {
  loading.value = true
  error.value = null
  getConnectionGraph(params())
    .then((g) => {
      return getConnectionLayout(g.scope)
        .then((lay) => {
          layout.value = lay
          applyGraph(g, lay.nodes)
        })
        .catch(() => applyGraph(g, {}))
    })
    .catch(() => {
      error.value = 'Failed to load connection graph. Choose a site, rack or device.'
    })
    .finally(() => {
      loading.value = false
    })
}

function persistLayout() {
  if (!graph.value?.scope || !canWrite.value) return
  const coords = {}
  for (const n of nodes.value) {
    coords[n.id] = { x: n.position.x, y: n.position.y }
  }
  saveConnectionLayout(graph.value.scope, { revision: layout.value.revision, nodes: coords })
    .then((lay) => {
      layout.value = lay
    })
    .catch((err) => {
      if (err.response?.status === 409) {
        toast.add({
          color: 'warning',
          title: 'Layout conflict',
          description: 'Reload to get the saved positions. Inventory was not changed.',
        })
        return
      }
      toast.add({ color: 'error', title: 'Could not save layout', description: errMsg(err) })
    })
}

function onNodesChange(changes) {
  if (changes.some((c) => c.type === 'position' && c.dragging === false)) persistLayout()
}

function onEdgeClick({ edge }) {
  selectedEdge.value = edge?.data || null
}

function setView(next) {
  view.value = next
  const query = { ...route.query, view: next }
  if (next === 'pair') {
    if (deviceAId.value) query.a = String(deviceAId.value)
    else delete query.a
    if (deviceBId.value) query.b = String(deviceBId.value)
    else delete query.b
  }
  router.replace({ query })
}

function deviceIdFromPicker(v) {
  if (v == null) return undefined
  if (typeof v === 'object') return v.id || undefined
  return v || undefined
}

function onDeviceA(id) {
  deviceAId.value = deviceIdFromPicker(id)
  setView('pair')
}

function onDeviceB(id) {
  deviceBId.value = deviceIdFromPicker(id)
  setView('pair')
}

function swapDevices() {
  const a = deviceAId.value
  deviceAId.value = deviceBId.value
  deviceBId.value = a
  setView('pair')
}

onMounted(() => {
  Promise.all([getSites(), getRacks(), getDevices()]).then(([s, r, d]) => {
    sites.value = s ?? []
    racks.value = r ?? []
    devices.value = d ?? []
  })
  if (view.value === 'graph' && (filters.value.site_id || filters.value.rack_id || filters.value.device_id)) {
    load()
  } else {
    loading.value = false
  }
})

watch(
  () => [
    filters.value.site_id,
    filters.value.rack_id,
    filters.value.device_id,
    filters.value.depth,
  ],
  () => {
    if (filters.value.site_id || filters.value.rack_id || filters.value.device_id) load()
  },
)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden">
    <div class="flex flex-wrap items-end gap-3 shrink-0">
      <h4 class="m-0 mr-2">Connections</h4>
      <div class="flex gap-1">
        <UButton
          v-for="item in viewItems"
          :key="item.value"
          size="sm"
          :variant="view === item.value ? 'solid' : 'ghost'"
          color="neutral"
          :label="item.label"
          @click="setView(item.value)"
        />
      </div>
    </div>

    <div v-if="view === 'pair'" class="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden">
      <div class="flex flex-wrap items-end gap-3 shrink-0">
        <UFormField label="Device A">
          <USelectMenu
            :model-value="deviceAId"
            :items="deviceItems"
            value-key="id"
            label-key="name"
            placeholder="Search device…"
            class="w-72"
            @update:model-value="onDeviceA"
          />
        </UFormField>
        <UButton
          icon="i-lucide-arrow-left-right"
          size="sm"
          color="neutral"
          variant="outline"
          :disabled="!deviceAId && !deviceBId"
          @click="swapDevices"
        />
        <UFormField label="Device B">
          <USelectMenu
            :model-value="deviceBId"
            :items="deviceItems"
            value-key="id"
            label-key="name"
            placeholder="Search device…"
            class="w-72"
            @update:model-value="onDeviceB"
          />
        </UFormField>
      </div>
      <DevicePairCables
        :device-a-id="deviceAId || 0"
        :device-b-id="deviceBId || 0"
        :can-write="canWrite"
      />
    </div>

    <template v-else>
    <div class="flex flex-wrap items-end gap-3 shrink-0">
      <UFormField label="Site">
        <USelect v-model="filters.site_id" :items="siteItems" class="w-48" />
      </UFormField>
      <UFormField label="Rack">
        <USelect v-model="filters.rack_id" :items="rackItems" class="w-48" />
      </UFormField>
      <UFormField label="Neighbour depth">
        <UInput v-model="filters.depth" type="number" class="w-24" />
      </UFormField>
      <UButton label="Load" size="sm" @click="load" />
      <UButton
        label="Fit"
        size="sm"
        variant="outline"
        color="neutral"
        @click="flowRef?.fitView?.()"
      />
    </div>
    <p class="text-muted text-sm shrink-0">
      Direct interface-to-interface cables only. This is not end-to-end tracing through patch panels
      or power. Rearranging nodes never changes inventory.
    </p>
    <div v-if="error" class="text-error text-sm">{{ error }}</div>
    <div class="min-h-0 flex-1 rounded border border-default overflow-hidden relative">
      <VueFlow
        ref="flowRef"
        v-model:nodes="nodes"
        v-model:edges="edges"
        :node-types="nodeTypes"
        :nodes-connectable="false"
        :edges-updatable="false"
        fit-view-on-init
        @nodes-change="onNodesChange"
        @edge-click="onEdgeClick"
      >
        <Background />
        <Controls />
      </VueFlow>
      <div v-if="loading" class="absolute inset-0 flex items-center justify-center bg-default/60">
        Loading…
      </div>
    </div>
    <USlideover
      :open="!!selectedEdge"
      title="Cable"
      @update:open="(v) => !v && (selectedEdge = null)"
    >
      <div v-if="selectedEdge" class="flex flex-col gap-3 p-4 text-sm">
        <div>{{ selectedEdge.label || 'Cable' }}</div>
        <div>Factum connection {{ selectedEdge.id }}</div>
        <UButton
          size="sm"
          variant="outline"
          label="Device A"
          @click="router.push({ path: '/device', query: { id: selectedEdge.device_a_id } })"
        />
        <UButton
          size="sm"
          variant="outline"
          label="Device B"
          @click="router.push({ path: '/device', query: { id: selectedEdge.device_b_id } })"
        />
        <p class="text-muted">Cables are read-only here. Change them in NetBox and sync.</p>
      </div>
    </USlideover>
    </template>
  </div>
</template>
