<script setup>
import { computed, ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  getOxidizedConfig,
  getOxidizedDiff,
  getOxidizedNodes,
  getOxidizedVersion,
  getOxidizedVersions,
} from '@/api/oxidized'
import {
  apiError,
  formatOxidizedTime,
  nodeKey,
  nodeMatches,
  statusColor,
} from '@/utils/oxidized'

const props = defineProps({
  // Already-resolved Oxidized node (Oxidized browser).
  node: { type: Object, default: null },
  // Factum device name to look up when the caller only has a device row
  // (DeviceList). Ignored when `node` is set.
  nodeName: { type: String, default: '' },
})

const open = defineModel('open', { type: Boolean, default: false })

const toast = useToast()

const PARENT_OID = '__parent__'

const resolved = ref(null)
const resolving = ref(false)
const resolveError = ref(null)

const shown = computed(() => props.node || resolved.value)

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

function resetContent() {
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
  resolved.value = null
  resolveError.value = null
}

function loadShownNode() {
  resetContent()
  if (props.node) {
    loadConfig(props.node)
    return
  }
  const want = (props.nodeName || '').trim()
  if (!want) {
    resolveError.value = 'No Oxidized node specified.'
    return
  }
  resolving.value = true
  getOxidizedNodes()
    .then((data) => {
      const match = (data ?? []).find((n) => nodeMatches(n, want))
      if (!match) {
        resolveError.value = `Device "${want}" is not in Oxidized.`
        return
      }
      resolved.value = match
      loadConfig(match)
    })
    .catch((err) => {
      resolveError.value = apiError(err, 'Failed to load Oxidized nodes.')
    })
    .finally(() => {
      resolving.value = false
    })
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
  if (!shown.value || !oidNew.value) {
    return
  }
  versionLoading.value = true
  versionError.value = null
  versionText.value = ''
  getOxidizedVersion(nodeKey(shown.value), oidNew.value)
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
  if (!shown.value || !oidNew.value) {
    return
  }
  diffLoading.value = true
  diffError.value = null
  diff.value = null
  getOxidizedDiff(
    nodeKey(shown.value),
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

watch(
  () => (open.value ? props.node || props.nodeName : null),
  (want) => {
    if (want) {
      loadShownNode()
    }
  },
)

watch(tab, (value) => {
  if (
    value === 'versions' &&
    shown.value &&
    versions.value.length === 0 &&
    !versionsLoading.value
  ) {
    loadVersions(shown.value)
  }
})

watch([oidNew, oidOld], () => {
  if (tab.value === 'versions' && oidNew.value) {
    loadVersionBlob()
    loadDiff()
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
</script>

<template>
  <UModal
    v-model:open="open"
    :title="shown?.name ? `${shown.name} — Oxidized` : 'Oxidized'"
    :ui="{ content: 'w-[95vw] h-[90vh] sm:max-w-none' }"
  >
    <template #body>
      <div v-if="resolving" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>
      <UAlert v-else-if="resolveError" color="error" variant="subtle" :title="resolveError" />
      <div v-else class="flex flex-col gap-3 min-h-0 h-full">
        <div class="flex flex-wrap gap-2 items-center shrink-0">
          <UBadge
            v-if="shown?.status"
            :label="shown.status"
            :color="statusColor(shown.status)"
            variant="subtle"
          />
          <span class="text-sm text-muted-color">{{ shown?.model }}</span>
          <span v-if="shown?.ip" class="text-sm text-muted-color">{{ shown.ip }}</span>
          <span
            v-if="shown?.group && shown.group !== 'default'"
            class="text-sm text-muted-color"
          >
            {{ shown.group }}
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
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="open = false" />
    </template>
  </UModal>
</template>
