<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { Wunderbaum } from 'wunderbaum'
import { useAuthStore } from '@/stores/auth'
import { getDevices } from '@/api/devices'
import {
  copySoftware,
  deleteSoftware,
  listSoftware,
  mkdirSoftware,
  moveSoftware,
  uploadSoftware,
} from '@/api/software'
import FormModal from '@/components/FormModal.vue'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

defineOptions({ name: 'SoftwarePage' })

const toast = useToast()
const authStore = useAuthStore()
const canWrite = computed(() => authStore.canWrite)

const el = ref(null)
const selected = ref(null)
const loading = ref(true)
const error = ref(null)
const uploading = ref(false)
const fileInput = ref(null)
let tree

const mkdirOpen = ref(false)
const mkdirName = ref('')
const mkdirSaving = ref(false)

const renameOpen = ref(false)
const renameFrom = ref('')
const renameTo = ref('')
const renameSaving = ref(false)

const copyOpen = ref(false)
const copyFrom = ref('')
const copyDevice = ref(undefined)
const copyProtocol = ref('http')
const copyDest = ref('')
const copySaving = ref(false)
const devices = ref([])

const folderIcon =
  '<i class="wb-icon"><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg></i>'
const folderOpenIcon =
  '<i class="wb-icon"><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m6 14l1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2"/></svg></i>'

const protocolItems = [
  { label: 'HTTP (device pull)', value: 'http' },
  { label: 'TFTP (device pull)', value: 'tftp' },
  { label: 'SCP (push from storage)', value: 'scp' },
  { label: 'SFTP (push from storage)', value: 'sftp' },
]

const deviceItems = computed(() => devices.value.map((d) => ({ label: d.name, value: d.name })))
const selectedIsFile = computed(() => selected.value && !selected.value.is_dir)
const selectedIsNode = computed(() => !!selected.value)

function errMsg(err, fallback) {
  return err.response?.data?.error ?? err.message ?? fallback
}

function joinPath(dir, name) {
  if (!dir || dir === '/') return '/' + name
  return dir.replace(/\/+$/, '') + '/' + name
}

function parentPath(p) {
  const parts = (p || '').split('/').filter(Boolean)
  parts.pop()
  return parts.length ? '/' + parts.join('/') : '/'
}

function targetDir() {
  const n = selected.value
  if (!n) return '/'
  if (n.is_dir) return n.path
  return parentPath(n.path)
}

function formatSize(row) {
  if (row?.is_dir) return ''
  const n = row?.size ?? 0
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MiB`
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GiB`
}

function formatTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d
    .toISOString()
    .replace('T', ' ')
    .replace(/\.\d+Z$/, ' UTC')
}

function payload(node) {
  if (!node) return null
  const data = node.data ?? {}
  return {
    name: data.name || node.title,
    path: data.path || node.key,
    is_dir: data.is_dir ?? node.type === 'folder',
    size: data.size ?? 0,
    mod_time: data.mod_time,
  }
}

function toWbNode(entry) {
  const isDir = !!entry.is_dir
  const lazy = isDir && entry.has_children !== false
  const node = {
    key: entry.path,
    title: entry.name,
    lazy,
    type: isDir ? 'folder' : 'file',
    name: entry.name,
    path: entry.path,
    is_dir: isDir,
    size: entry.size ?? 0,
    mod_time: entry.mod_time,
  }
  if (!lazy) node.children = []
  return node
}

function sortEntries(rows) {
  return (rows ?? []).slice().sort((a, b) => {
    if (!!a.is_dir !== !!b.is_dir) return a.is_dir ? -1 : 1
    return (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' })
  })
}

function renderCell(e) {
  const data = e.node.data ?? {}
  for (const col of Object.values(e.renderColInfosById ?? {})) {
    if (col.id === 'kind') {
      col.elem.textContent = e.node.type === 'folder' ? 'Folder' : 'File'
    } else if (col.id === 'size') {
      col.elem.textContent = formatSize(data)
    } else if (col.id === 'mod_time') {
      col.elem.textContent = formatTime(data.mod_time)
    }
  }
}

function destroyTree() {
  if (tree) {
    tree.resizeObserver?.disconnect()
    tree = null
  }
}

function isDescendantPath(parent, child) {
  if (!parent || !child) return false
  if (parent === '/') return child !== '/'
  return child === parent || child.startsWith(parent + '/')
}

function buildTree(source) {
  if (!el.value) return
  tree = new Wunderbaum({
    element: el.value,
    id: 'software',
    header: true,
    debugLevel: 0,
    rowHeightPx: 28,
    navigationModeOption: 'row',
    iconMap: {
      ...Wunderbaum.iconMaps?.bootstrap,
      expanderExpanded: '<i class="wb-expander">−</i>',
      expanderCollapsed: '<i class="wb-expander">+</i>',
      expanderLazy: '<i class="wb-expander">+</i>',
      folder: folderIcon,
      folderOpen: folderOpenIcon,
      folderLazy: folderIcon,
    },
    source,
    columns: [
      { id: '*', title: 'Name', width: '*' },
      { id: 'kind', title: 'Type', width: '90px' },
      { id: 'size', title: 'Size', width: '100px' },
      { id: 'mod_time', title: 'Modified', width: '180px' },
    ],
    types: {
      folder: { icon: true },
      file: { icon: false },
    },
    lazyLoad: (e) => listSoftware(e.node.key).then((rows) => sortEntries(rows).map(toWbNode)),
    render: renderCell,
    activate: (e) => {
      selected.value = payload(e.node)
    },
    deactivate: () => {
      selected.value = null
    },
    dnd: canWrite.value
      ? {
          dragStart: () => true,
          dragEnter: (e) => (e.node.type === 'folder' ? ['over'] : false),
          drop: (e) => {
            const source = e.sourceNode
            const target = e.node
            if (!source || !target || target.type !== 'folder') return
            const from = source.data?.path || source.key
            const destDir = target.data?.path || target.key
            if (from === destDir || isDescendantPath(from, destDir)) return
            const to = joinPath(destDir, source.data?.name || source.title)
            moveSoftware(from, to)
              .then(() => refreshDir(parentPath(from), destDir))
              .catch((err) => toast.add({ title: errMsg(err, 'Could not move'), color: 'error' }))
          },
        }
      : undefined,
  })
}

function load() {
  loading.value = true
  error.value = null
  return listSoftware('/')
    .then((rows) => {
      const source = sortEntries(rows).map(toWbNode)
      if (tree) {
        return tree.load(source)
      }
      buildTree(source)
    })
    .catch((err) => {
      error.value = errMsg(err, 'Failed to list the repository.')
      if (!tree) buildTree([])
    })
    .finally(() => {
      loading.value = false
    })
}

async function refreshDir(...dirs) {
  const keep = selected.value?.path
  const unique = [...new Set(dirs.filter(Boolean))]
  const needRoot = unique.some((d) => d === '/')
  if (needRoot || !tree) {
    await load()
  }
  for (const dir of unique) {
    if (dir === '/') continue
    const node = tree?.findKey(dir)
    if (!node) continue
    node.resetLazy()
    await node.setExpanded(true)
  }
  if (keep) {
    const node = tree?.findKey(keep)
    if (node) node.setActive()
  }
}

function openMkdir() {
  mkdirName.value = ''
  mkdirOpen.value = true
}

function saveMkdir() {
  const name = mkdirName.value.trim()
  if (!name) return
  const dir = targetDir()
  mkdirSaving.value = true
  mkdirSoftware(joinPath(dir, name))
    .then(() => {
      mkdirOpen.value = false
      return refreshDir(dir)
    })
    .catch((err) => toast.add({ title: errMsg(err, 'Could not create folder'), color: 'error' }))
    .finally(() => {
      mkdirSaving.value = false
    })
}

function openRename() {
  const n = selected.value
  if (!n) return
  renameFrom.value = n.path
  renameTo.value = n.name
  renameOpen.value = true
}

function saveRename() {
  const name = renameTo.value.trim()
  if (!name) return
  const from = renameFrom.value
  const parent = parentPath(from)
  renameSaving.value = true
  moveSoftware(from, joinPath(parent, name))
    .then(() => {
      renameOpen.value = false
      selected.value = null
      return refreshDir(parent)
    })
    .catch((err) => toast.add({ title: errMsg(err, 'Could not rename'), color: 'error' }))
    .finally(() => {
      renameSaving.value = false
    })
}

function remove() {
  const n = selected.value
  if (!n) return
  if (!confirm(`Delete ${n.path}?`)) return
  const parent = parentPath(n.path)
  deleteSoftware(n.path)
    .then(() => {
      selected.value = null
      return refreshDir(parent)
    })
    .catch((err) => toast.add({ title: errMsg(err, 'Could not delete'), color: 'error' }))
}

function pickFiles() {
  fileInput.value?.click()
}

function onFiles(ev) {
  const files = [...(ev.target.files ?? [])]
  ev.target.value = ''
  if (!files.length) return
  const dir = targetDir()
  uploading.value = true
  Promise.all(files.map((f) => uploadSoftware(joinPath(dir, f.name), f)))
    .then(() => refreshDir(dir))
    .catch((err) => toast.add({ title: errMsg(err, 'Upload failed'), color: 'error' }))
    .finally(() => {
      uploading.value = false
    })
}

function openCopy() {
  const n = selected.value
  if (!n || n.is_dir) return
  copyFrom.value = n.path
  copyDest.value = n.name
  copyProtocol.value = 'http'
  copyDevice.value = undefined
  copyOpen.value = true
  if (!devices.value.length) {
    getDevices()
      .then((data) => {
        devices.value = data ?? []
      })
      .catch((err) => toast.add({ title: errMsg(err, 'Failed to load devices'), color: 'error' }))
  }
}

function saveCopy() {
  if (!copyDevice.value) return
  copySaving.value = true
  copySoftware({
    path: copyFrom.value,
    device: copyDevice.value,
    protocol: copyProtocol.value,
    destination: copyDest.value,
  })
    .then(() => {
      copyOpen.value = false
      toast.add({
        title: 'Copy started',
        description: 'Progress is in the log panel. The storage worker runs the transfer.',
        color: 'success',
      })
    })
    .catch((err) => toast.add({ title: errMsg(err, 'Could not start copy'), color: 'error' }))
    .finally(() => {
      copySaving.value = false
    })
}

onMounted(() => {
  nextTick(() => load())
})
onBeforeUnmount(destroyTree)
</script>

<template>
  <div class="card">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-4">
      <div>
        <div class="font-semibold text-lg">Software</div>
        <small class="text-muted-color"
          >Router and switch images. HTTP and TFTP are pulled by the device; SCP and SFTP are pushed
          from the storage host.</small
        >
      </div>
      <div v-if="canWrite" class="flex flex-wrap gap-2">
        <UButton
          label="New folder"
          icon="i-lucide-folder-plus"
          color="neutral"
          variant="outline"
          @click="openMkdir"
        />
        <UButton label="Upload" icon="i-lucide-upload" :loading="uploading" @click="pickFiles" />
        <UButton
          label="Copy to device"
          icon="i-lucide-hard-drive-download"
          color="neutral"
          variant="outline"
          :disabled="!selectedIsFile"
          @click="openCopy"
        />
        <UButton
          label="Rename"
          icon="i-lucide-pencil"
          color="neutral"
          variant="outline"
          :disabled="!selectedIsNode"
          @click="openRename"
        />
        <UButton
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="outline"
          :disabled="!selectedIsNode"
          @click="remove"
        />
        <input ref="fileInput" type="file" class="hidden" multiple @change="onFiles" />
      </div>
    </div>

    <p v-if="canWrite" class="text-sm text-muted-color mb-3">
      New folder and upload go into
      <span class="font-mono">{{ targetDir() }}</span
      >. Drag a row onto a folder to move it.
    </p>

    <div v-if="error" class="mb-4">
      <UAlert color="error" variant="subtle" :title="error" />
    </div>

    <div v-if="loading" class="flex justify-center p-3">
      <UIcon name="i-lucide-loader-2" class="size-6 animate-spin" />
    </div>

    <div ref="el" class="ipam-tree min-h-96 h-[min(70vh,40rem)]" />
  </div>

  <FormModal v-model:open="mkdirOpen" :source="mkdirName" title="New folder">
    <template #body>
      <form id="software-mkdir" class="space-y-3" @submit.prevent="saveMkdir">
        <p class="text-sm text-muted-color">{{ targetDir() }}</p>
        <UFormField label="Name">
          <UInput v-model="mkdirName" class="w-full" required />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <UButton color="neutral" variant="ghost" type="button" @click="mkdirOpen = false"
        >Cancel</UButton
      >
      <UButton type="submit" form="software-mkdir" :loading="mkdirSaving">Create</UButton>
    </template>
  </FormModal>

  <FormModal v-model:open="renameOpen" :source="renameTo" title="Rename">
    <template #body>
      <form id="software-rename" class="space-y-3" @submit.prevent="saveRename">
        <UFormField label="New name">
          <UInput v-model="renameTo" class="w-full" required />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <UButton color="neutral" variant="ghost" type="button" @click="renameOpen = false"
        >Cancel</UButton
      >
      <UButton type="submit" form="software-rename" :loading="renameSaving">Rename</UButton>
    </template>
  </FormModal>

  <FormModal v-model:open="copyOpen" :source="copyDest" title="Copy to device">
    <template #body>
      <form id="software-copy" class="space-y-3" @submit.prevent="saveCopy">
        <p class="text-sm text-muted-color">{{ copyFrom }}</p>
        <UFormField label="Device">
          <USelect v-model="copyDevice" :items="deviceItems" class="w-full" />
        </UFormField>
        <UFormField label="Protocol">
          <USelect v-model="copyProtocol" :items="protocolItems" class="w-full" />
        </UFormField>
        <UFormField label="On-device destination">
          <UInput v-model="copyDest" class="w-full" placeholder="flash:image.bin" />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <UButton color="neutral" variant="ghost" type="button" @click="copyOpen = false"
        >Cancel</UButton
      >
      <UButton type="submit" form="software-copy" :loading="copySaving">Copy</UButton>
    </template>
  </FormModal>
</template>

<style scoped>
:deep(.ipam-tree span.wb-node i.wb-icon) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  width: 14px;
  height: 14px;
  margin: 0 6px 0 0;
  padding: 0;
  flex-shrink: 0;
  color: var(--ui-text-muted, var(--ui-text-dimmed, var(--ui-text)));
}

:deep(.ipam-tree span.wb-node i.wb-icon svg) {
  display: block;
  width: 100%;
  height: 100%;
}
</style>
