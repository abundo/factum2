<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { getDevices } from '@/api/devices'
import {
  commitRunningConfig,
  getRunningConfig,
  getRunningConfigPlatforms,
} from '@/api/runningConfig'
import ConfigTextEditor from '@/components/ConfigTextEditor.vue'
import { confirmDiscard, useUnsaved } from '@/composables/useFormGuard'
import { useAuthStore } from '@/stores/auth'
import { normalizeConfigText, removedLineCount } from '@/utils/configText'

defineOptions({ name: 'RunningConfigPage' })

const toast = useToast()
const auth = useAuthStore()
const route = useRoute()

const platforms = ref([])
const devices = ref([])
const deviceId = ref(null)
const loading = ref(false)
const committing = ref(false)
const error = ref('')
const contexts = ref([])
const originals = ref({})
const drafts = ref({})
const selectedId = ref('root')
const expanded = ref(new Set())
const result = ref(null)

const deviceItems = computed(() =>
  devices.value.map((d) => ({ label: `${d.name} (${d.platform})`, value: d.id })),
)

function findNode(nodes, id) {
  for (const node of nodes || []) {
    if (node.id === id) return node
    const child = findNode(node.children, id)
    if (child) return child
  }
  return null
}

const selected = computed(() => findNode(contexts.value, selectedId.value))

const editorText = computed({
  get: () => drafts.value[selectedId.value] ?? '',
  set: (value) => {
    drafts.value[selectedId.value] = value
  },
})

const selectedOriginal = computed(() => originals.value[selectedId.value] ?? '')

function nodeDirty(id) {
  return normalizeConfigText(drafts.value[id] ?? '') !== normalizeConfigText(originals.value[id] ?? '')
}

function subtreeDirty(node) {
  if (!node) return false
  if (node.editable && nodeDirty(node.id)) return true
  return (node.children || []).some(subtreeDirty)
}

const removed = computed(() =>
  selected.value?.editable ? removedLineCount(selectedOriginal.value, editorText.value) : 0,
)

const canCommit = computed(
  () =>
    auth.canWrite &&
    selected.value?.editable &&
    nodeDirty(selected.value.id) &&
    !loading.value &&
    !committing.value,
)

const pageDirty = computed(() => contexts.value.some(subtreeDirty))

useUnsaved(
  () => pageDirty.value,
  () => {
    drafts.value = { ...originals.value }
  },
)

function remember(nodes, orig, draft) {
  for (const node of nodes || []) {
    const body = node.body ?? ''
    orig[node.id] = body
    draft[node.id] = body
    remember(node.children, orig, draft)
  }
}

function applyTree(next) {
  const orig = {}
  const draft = {}
  remember(next, orig, draft)
  contexts.value = next || []
  originals.value = orig
  drafts.value = draft
  if (!findNode(contexts.value, selectedId.value)) selectedId.value = 'root'
}

const rows = computed(() => {
  const out = []
  const walk = (nodes, depth) => {
    for (const node of nodes || []) {
      out.push({ node, depth })
      if (node.children?.length && expanded.value.has(node.id)) walk(node.children, depth + 1)
    }
  }
  walk(contexts.value, 0)
  return out
})

function toggle(id) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
}

function expandSubtree(node, set) {
  if (!node?.children?.length) return
  set.add(node.id)
  for (const child of node.children) expandSubtree(child, set)
}

function onRow(node) {
  if (node.editable) selectedId.value = node.id
  if (!node.children?.length) return
  const next = new Set(expanded.value)
  expandSubtree(node, next)
  expanded.value = next
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? err.message ?? fallback
}

async function loadDevices() {
  const [plats, list] = await Promise.all([getRunningConfigPlatforms(), getDevices()])
  platforms.value = (plats ?? []).map((p) => String(p).toLowerCase())
  devices.value = (list ?? []).filter((d) => platforms.value.includes(String(d.platform ?? '').toLowerCase()))
}

async function loadConfig() {
  if (!deviceId.value) return
  loading.value = true
  error.value = ''
  try {
    const data = await getRunningConfig(deviceId.value)
    applyTree(data.contexts)
  } catch (err) {
    contexts.value = []
    originals.value = {}
    drafts.value = {}
    error.value = errMsg(err, 'Failed to read the running configuration.')
  } finally {
    loading.value = false
  }
}

async function pickDevice(id) {
  const next = id == null || id === '' ? null : Number(id)
  if (next === deviceId.value) return
  if (pageDirty.value && !(await confirmDiscard())) return
  deviceId.value = next
  result.value = null
  error.value = ''
  contexts.value = []
  expanded.value = new Set()
  if (next) await loadConfig()
}

async function refresh() {
  if (!deviceId.value) return
  if (pageDirty.value && !(await confirmDiscard())) return
  result.value = null
  await loadConfig()
}

async function commit() {
  if (!canCommit.value) return
  committing.value = true
  error.value = ''
  const id = selected.value.id
  try {
    const data = await commitRunningConfig(deviceId.value, {
      context_id: id,
      body: drafts.value[id] ?? '',
      base_body: originals.value[id] ?? '',
    })
    applyTree(data.contexts)
    if (findNode(contexts.value, id)) selectedId.value = id
    result.value = {
      ok: true,
      diff: data.diff || '',
      label: findNode(contexts.value, id)?.label || id,
    }
    toast.add({ title: 'Configuration committed', color: 'success' })
  } catch (err) {
    error.value = errMsg(err, 'Commit failed. The change was aborted.')
  } finally {
    committing.value = false
  }
}

function diffClass(line) {
  if (line.startsWith('+') && !line.startsWith('+++')) return 'text-green-700 dark:text-green-400'
  if (line.startsWith('-') && !line.startsWith('---')) return 'text-red-600 dark:text-red-400'
  return 'text-muted'
}

const enterLabel = computed(() => {
  const enter = selected.value?.enter ?? []
  if (!enter.length) return 'Global configuration. Child contexts are edited on their own nodes.'
  return enter.join(' ↵ ')
})

onMounted(async () => {
  try {
    await loadDevices()
  } catch (err) {
    error.value = errMsg(err, 'Failed to load devices.')
    return
  }
  const queryId = Number(route.query.device)
  if (queryId && devices.value.some((d) => d.id === queryId)) {
    deviceId.value = queryId
    await loadConfig()
  }
})
</script>

<template>
  <div class="flex h-[calc(100vh-7rem)] min-h-0 flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <h4 class="m-0 mr-2">Configuration</h4>
      <USelect
        :model-value="deviceId"
        :items="deviceItems"
        placeholder="Device"
        class="w-full sm:w-80"
        @update:model-value="pickDevice"
      />
      <UButton
        icon="i-lucide-refresh-cw"
        variant="outline"
        color="neutral"
        :loading="loading"
        :disabled="!deviceId || committing"
        @click="refresh"
      >
        Refresh
      </UButton>
      <div class="flex-1" />
      <UButton v-if="auth.canWrite" :disabled="!canCommit" :loading="committing" @click="commit">
        Commit
      </UButton>
    </div>

    <p v-if="error" class="text-sm text-red-500">{{ error }}</p>

    <div
      v-if="result"
      class="max-h-40 shrink-0 overflow-auto rounded-md border border-default p-2"
    >
      <div class="mb-1 flex items-center gap-2">
        <span class="text-sm font-medium">Changes in {{ result.label }}</span>
        <UButton
          icon="i-lucide-x"
          size="xs"
          variant="ghost"
          color="neutral"
          class="ml-auto"
          @click="result = null"
        />
      </div>
      <p v-if="!result.diff" class="text-sm text-muted">
        Committed. This context matches the text from before the commit.
      </p>
      <div v-else class="font-mono text-xs leading-5 whitespace-pre">
        <div v-for="(line, i) in result.diff.split('\n')" :key="i" :class="diffClass(line)">{{ line || ' ' }}</div>
      </div>
    </div>

    <div class="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
      <div class="max-h-56 shrink-0 overflow-auto rounded-md border border-default lg:max-h-none lg:w-72">
        <p v-if="!deviceId" class="p-3 text-sm text-muted">Select a device.</p>
        <p v-else-if="loading && !rows.length" class="p-3 text-sm text-muted">Loading configuration…</p>
        <button
          v-for="row in rows"
          :key="row.node.id"
          type="button"
          class="flex w-full items-center gap-1 rounded px-2 py-1 text-left text-sm hover:bg-elevated"
          :class="row.node.id === selectedId && row.node.editable ? 'bg-elevated' : ''"
          :style="{ paddingLeft: `${8 + row.depth * 14}px` }"
          @click="onRow(row.node)"
        >
          <span
            v-if="row.node.children?.length"
            class="inline-flex w-4 shrink-0 justify-center text-muted"
            @click.stop="toggle(row.node.id)"
          >{{ expanded.has(row.node.id) ? '▾' : '▸' }}</span>
          <span v-else class="w-4 shrink-0" />
          <span class="truncate font-mono" :class="row.node.editable ? '' : 'font-sans font-medium'">{{
            row.node.label
          }}</span>
          <span
            v-if="subtreeDirty(row.node)"
            class="ml-auto h-2 w-2 shrink-0 rounded-full bg-amber-500"
            title="Modified"
          />
        </button>
      </div>

      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-md border border-default">
        <div v-if="selected?.editable" class="flex items-center gap-2 border-b border-default px-3 py-2">
          <span class="truncate font-mono text-sm">{{ selected.label }}</span>
          <span
            v-if="nodeDirty(selected.id)"
            class="rounded bg-amber-500/15 px-1.5 py-0.5 text-xs text-amber-700 dark:text-amber-300"
          >Modified</span>
          <span v-if="removed" class="text-xs text-muted">{{ removed }} removed</span>
        </div>
        <p v-if="selected?.editable" class="border-b border-default px-3 py-1.5 font-mono text-xs text-muted">
          {{ enterLabel }}
        </p>
        <ConfigTextEditor
          v-if="selected?.editable"
          :key="`${deviceId}:${selected.id}`"
          v-model="editorText"
          :original="selectedOriginal"
          class="min-h-0 flex-1"
        />
        <p v-else class="p-3 text-sm text-muted">
          {{ deviceId ? 'Select a context.' : 'Select a device to read its running configuration.' }}
        </p>
      </div>
    </div>
  </div>
</template>
