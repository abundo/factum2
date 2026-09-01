<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import {
  getOxidizedConfig,
  getOxidizedDiff,
  getOxidizedNodes,
  getOxidizedVersion,
  getOxidizedVersions,
} from '@/api/oxidized'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { formatDateTime } from '@/utils/datetime'

defineOptions({ name: 'OxidizedBrowserPage' })

const toast = useToast()
const route = useRoute()
const router = useRouter()

const nodes = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'ip', header: 'IP' },
  { accessorKey: 'model', header: 'Model' },
  { accessorKey: 'group', header: 'Group' },
  { accessorKey: 'status', header: 'Status' },
  { accessorKey: 'time', header: 'Last backup' },
  { accessorKey: 'mtime', header: 'Last change' },
]

const detailOpen = ref(false)
const selected = ref(null)
const tab = ref('config')
const tabItems = [
  { label: 'Config', value: 'config', slot: 'config' },
  { label: 'Versions', value: 'versions', slot: 'versions' },
]

const configText = ref('')
const configLoading = ref(false)
const configError = ref(null)

const versions = ref([])
const versionsLoading = ref(false)
const versionsError = ref(null)
const PARENT_OID = '__parent__'
const oidNew = ref('')
const oidOld = ref(PARENT_OID)

const versionText = ref('')
const versionLoading = ref(false)
const versionError = ref(null)

const diff = ref(null)
const diffLoading = ref(false)
const diffError = ref(null)

const versionColumns = [
  { id: 'pick', header: '' },
  { accessorKey: 'date', header: 'Date' },
  { accessorKey: 'author', header: 'Author' },
  { accessorKey: 'message', header: 'Message' },
  { accessorKey: 'oid', header: 'Commit' },
]

function statusColor(status) {
  switch ((status ?? '').toLowerCase()) {
    case 'success':
      return 'success'
    case 'never':
      return 'neutral'
    case 'no_connection':
    case 'fail':
      return 'error'
    default:
      return 'warning'
  }
}

function formatOxidizedTime(value) {
  if (!value || value === 'never') {
    return value === 'never' ? 'never' : '—'
  }
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) {
    return value
  }
  return formatDateTime(d)
}

function apiError(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function load() {
  loading.value = true
  error.value = null
  getOxidizedNodes()
    .then((data) => {
      nodes.value = data ?? []
    })
    .catch((err) => {
      error.value = apiError(err, 'Failed to load Oxidized nodes.')
      nodes.value = []
    })
    .finally(() => {
      loading.value = false
    })
}

function nodeKey(node) {
  return node?.full_name || node?.name || ''
}

// Oxidized stores FQDNs; factum device names are often the short hostname.
function nodeMatches(node, want) {
  if (!want) {
    return false
  }
  if (nodeKey(node) === want || node.name === want || node.ip === want) {
    return true
  }
  const wantHost = String(want).split('/').pop()
  if (node.name === wantHost) {
    return true
  }
  return (node.name || '').split('.')[0] === wantHost.split('.')[0]
}

function openNode(node) {
  selected.value = node
  detailOpen.value = true
  tab.value = 'config'
  oidNew.value = ''
  oidOld.value = PARENT_OID
  configText.value = ''
  configError.value = null
  versions.value = []
  versionsError.value = null
  versionText.value = ''
  versionError.value = null
  diff.value = null
  diffError.value = null
  loadConfig(node)
  const key = nodeKey(node)
  if (route.query.node !== key) {
    router.replace({ query: { ...route.query, node: key } })
  }
}

function loadConfig(node) {
  configLoading.value = true
  configError.value = null
  getOxidizedConfig(nodeKey(node))
    .then((data) => {
      configText.value = data?.config ?? ''
    })
    .catch((err) => {
      configError.value = apiError(err, 'Failed to load configuration.')
    })
    .finally(() => {
      configLoading.value = false
    })
}

function loadVersions(node) {
  versionsLoading.value = true
  versionsError.value = null
  getOxidizedVersions(nodeKey(node))
    .then((data) => {
      versions.value = data ?? []
      if (versions.value.length > 0 && !oidNew.value) {
        oidNew.value = versions.value[0].oid
        oidOld.value = versions.value[1]?.oid ?? PARENT_OID
      }
    })
    .catch((err) => {
      versionsError.value = apiError(err, 'Failed to load versions.')
      versions.value = []
    })
    .finally(() => {
      versionsLoading.value = false
    })
}

function loadVersionBlob() {
  if (!selected.value || !oidNew.value) {
    return
  }
  versionLoading.value = true
  versionError.value = null
  versionText.value = ''
  getOxidizedVersion(nodeKey(selected.value), oidNew.value)
    .then((data) => {
      versionText.value = data?.config ?? ''
    })
    .catch((err) => {
      versionError.value = apiError(err, 'Failed to load version.')
    })
    .finally(() => {
      versionLoading.value = false
    })
}

function loadDiff() {
  if (!selected.value || !oidNew.value) {
    return
  }
  diffLoading.value = true
  diffError.value = null
  diff.value = null
  getOxidizedDiff(
    nodeKey(selected.value),
    oidNew.value,
    oidOld.value === PARENT_OID ? '' : oidOld.value,
  )
    .then((data) => {
      diff.value = data
    })
    .catch((err) => {
      diffError.value = apiError(err, 'Failed to load diff.')
    })
    .finally(() => {
      diffLoading.value = false
    })
}

watch(tab, (value) => {
  if (
    value === 'versions' &&
    selected.value &&
    versions.value.length === 0 &&
    !versionsLoading.value
  ) {
    loadVersions(selected.value)
  }
})

watch([oidNew, oidOld], () => {
  if (tab.value === 'versions' && oidNew.value) {
    loadVersionBlob()
    loadDiff()
  }
})

watch(detailOpen, (open) => {
  if (!open && route.query.node) {
    const q = { ...route.query }
    delete q.node
    router.replace({ query: q })
  }
})

function copyText(text) {
  if (!text) {
    return
  }
  navigator.clipboard
    .writeText(text)
    .then(() => {
      toast.add({ color: 'success', title: 'Copied', duration: 2000 })
    })
    .catch(() => {
      toast.add({ color: 'error', title: 'Copy failed', duration: 3000 })
    })
}

function diffLineClass(line) {
  if (line.startsWith('+++') || line.startsWith('---')) {
    return 'text-muted'
  }
  if (line.startsWith('+')) {
    return 'bg-success/15 text-success'
  }
  if (line.startsWith('-')) {
    return 'bg-error/15 text-error'
  }
  if (line.startsWith('@@')) {
    return 'text-info'
  }
  return ''
}

const diffLines = computed(() => {
  const patch = diff.value?.patch
  if (!patch) {
    return []
  }
  return patch.replace(/\n$/, '').split('\n')
})

onMounted(() => {
  load()
})

watch(
  () => [loading.value, nodes.value, route.query.node],
  () => {
    const want = route.query.node
    if (loading.value || !want || detailOpen.value) {
      return
    }
    const match = nodes.value.find((n) => nodeMatches(n, want))
    if (match) {
      openNode(match)
    }
  },
)
</script>

<template>
  <div class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <h4 class="m-0">Oxidized</h4>
      <div class="flex gap-2 items-center">
        <UButton
          icon="i-lucide-refresh-cw"
          variant="outline"
          color="neutral"
          size="sm"
          :loading="loading"
          @click="load"
        />
        <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
      </div>
    </div>

    <p class="text-muted-color mb-4">
      Device configurations stored by Oxidized, including version history and diffs. Oxidized's API
      only lists devices currently in its source (router.db); configs of removed devices stay in git
      unless
      <span class="font-mono">clean_obsolete_nodes</span>
      is enabled, but they cannot be browsed here.
    </p>

    <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-4" />

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="nodes"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No devices in Oxidized.'"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-320px)]"
    >
      <template
        v-for="col in columns.filter((c) => c.id !== 'actions')"
        :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }"
      >
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          label="Browse"
          icon="i-lucide-file-search"
          size="sm"
          variant="outline"
          color="neutral"
          @click="openNode(row.original)"
        />
      </template>
      <template #status-cell="{ row }">
        <UBadge
          v-if="row.original.status"
          :label="row.original.status"
          :color="statusColor(row.original.status)"
          variant="subtle"
        />
      </template>
      <template #time-cell="{ row }">
        {{ formatOxidizedTime(row.original.time) }}
      </template>
      <template #mtime-cell="{ row }">
        {{ formatOxidizedTime(row.original.mtime) }}
      </template>
    </UTable>
  </div>

  <UModal
    v-model:open="detailOpen"
    :title="selected?.name ? `${selected.name} — Oxidized` : 'Oxidized'"
    :ui="{ content: 'w-[95vw] h-[90vh] sm:max-w-none' }"
  >
    <template #body>
      <div class="flex flex-col gap-3 min-h-0 h-full">
        <div class="flex flex-wrap gap-2 items-center shrink-0">
          <UBadge
            v-if="selected?.status"
            :label="selected.status"
            :color="statusColor(selected.status)"
            variant="subtle"
          />
          <span class="text-sm text-muted-color">{{ selected?.model }}</span>
          <span v-if="selected?.ip" class="text-sm text-muted-color">{{ selected.ip }}</span>
          <span
            v-if="selected?.group && selected.group !== 'default'"
            class="text-sm text-muted-color"
          >
            {{ selected.group }}
          </span>
        </div>

        <UTabs v-model="tab" :items="tabItems" class="min-h-0 flex-1">
          <template #config>
            <div class="flex flex-col gap-2 pt-3 min-h-0">
              <div class="flex justify-end">
                <UButton
                  label="Copy"
                  icon="i-lucide-copy"
                  size="sm"
                  variant="outline"
                  color="neutral"
                  :disabled="!configText"
                  @click="copyText(configText)"
                />
              </div>
              <div v-if="configLoading" class="flex justify-center p-4">
                <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
              </div>
              <UAlert v-else-if="configError" color="error" variant="subtle" :title="configError" />
              <pre
                v-else
                class="font-mono text-xs overflow-auto max-h-[calc(90vh-220px)] p-3 rounded-md bg-muted whitespace-pre"
                >{{ configText || '(empty)' }}</pre
              >
            </div>
          </template>

          <template #versions>
            <div class="flex flex-col gap-3 pt-3 min-h-0">
              <div v-if="versionsLoading" class="flex justify-center p-4">
                <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
              </div>
              <UAlert
                v-else-if="versionsError"
                color="error"
                variant="subtle"
                :title="versionsError"
              />
              <template v-else>
                <div class="flex flex-wrap gap-2 items-end">
                  <div>
                    <div class="text-sm font-bold mb-1">New</div>
                    <USelect
                      v-model="oidNew"
                      :items="
                        versions.map((v) => ({
                          label: `${formatOxidizedTime(v.date || v.time)} (${v.oid.slice(0, 8)})`,
                          value: v.oid,
                        }))
                      "
                      class="min-w-64"
                    />
                  </div>
                  <div>
                    <div class="text-sm font-bold mb-1">Old</div>
                    <USelect
                      v-model="oidOld"
                      :items="[
                        { label: 'Previous commit', value: PARENT_OID },
                        ...versions.map((v) => ({
                          label: `${formatOxidizedTime(v.date || v.time)} (${v.oid.slice(0, 8)})`,
                          value: v.oid,
                        })),
                      ]"
                      class="min-w-64"
                    />
                  </div>
                  <UButton
                    label="Copy config"
                    icon="i-lucide-copy"
                    size="sm"
                    variant="outline"
                    color="neutral"
                    :disabled="!versionText"
                    @click="copyText(versionText)"
                  />
                </div>

                <UTable
                  :data="versions"
                  :columns="versionColumns"
                  :empty="'No versions.'"
                  class="max-h-48"
                >
                  <template #pick-cell="{ row }">
                    <UButton
                      label="New"
                      size="xs"
                      variant="outline"
                      color="neutral"
                      @click="oidNew = row.original.oid"
                    />
                  </template>
                  <template #date-cell="{ row }">
                    {{ formatOxidizedTime(row.original.date || row.original.time) }}
                  </template>
                  <template #author-cell="{ row }">
                    {{ row.original.author?.name || '—' }}
                  </template>
                  <template #oid-cell="{ row }">
                    <span class="font-mono text-xs">{{ row.original.oid?.slice(0, 12) }}</span>
                  </template>
                </UTable>

                <div class="grid grid-cols-1 lg:grid-cols-2 gap-3 min-h-0">
                  <div class="min-h-0">
                    <div class="font-semibold mb-2">Version config</div>
                    <div v-if="versionLoading" class="flex justify-center p-4">
                      <UIcon name="i-lucide-loader-2" class="size-6 animate-spin" />
                    </div>
                    <UAlert
                      v-else-if="versionError"
                      color="error"
                      variant="subtle"
                      :title="versionError"
                    />
                    <pre
                      v-else
                      class="font-mono text-xs overflow-auto max-h-[calc(90vh-430px)] p-3 rounded-md bg-muted whitespace-pre"
                      >{{ versionText || '(empty)' }}</pre
                    >
                  </div>
                  <div class="min-h-0">
                    <div class="font-semibold mb-2 flex gap-2 items-center">
                      <span>Diff</span>
                      <UBadge
                        v-if="diff"
                        :label="`+${diff.added ?? 0} / -${diff.removed ?? 0}`"
                        color="neutral"
                        variant="subtle"
                      />
                    </div>
                    <div v-if="diffLoading" class="flex justify-center p-4">
                      <UIcon name="i-lucide-loader-2" class="size-6 animate-spin" />
                    </div>
                    <UAlert
                      v-else-if="diffError"
                      color="error"
                      variant="subtle"
                      :title="diffError"
                    />
                    <div v-else-if="diffLines.length === 0" class="text-muted-color text-sm p-3">
                      No diff available.
                    </div>
                    <pre
                      v-else
                      class="font-mono text-xs overflow-auto max-h-[calc(90vh-430px)] p-3 rounded-md bg-muted"
                    ><span
                        v-for="(line, i) in diffLines"
                        :key="i"
                        class="block whitespace-pre"
                        :class="diffLineClass(line)"
                        >{{ line || ' ' }}</span
                      ></pre>
                  </div>
                </div>
              </template>
            </div>
          </template>
        </UTabs>
      </div>
    </template>
    <template #footer>
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="detailOpen = false" />
    </template>
  </UModal>
</template>
