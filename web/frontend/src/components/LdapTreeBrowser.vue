<script setup>
import { ref, watch } from 'vue'
import { browseLdap } from '@/api/ldap'

// Dialog that lets an admin navigate the configured LDAP/AD directory tree
// and pick a group DN, instead of having to know/type it exactly. Generic
// over any dn-shaped field - callers just listen for @select and assign the
// emitted DN wherever it belongs, so this can be reused anywhere else a DN
// needs picking, not just the group -> role mapping dialog.
const props = defineProps({
  visible: { type: Boolean, default: false },
})
const emit = defineEmits(['update:visible', 'select'])

const items = ref([])
const loading = ref(false)
const error = ref(null)
const expandedKeys = ref([])
// UTree's v-model holds the whole selected node object (as passed via
// TreeItem's `:value="item"`), not just its `.value` DN string.
const selectedNode = ref(null)

// UTree infers whether a node is expandable from `children.length`, so an
// unloaded-but-expandable container gets a disabled placeholder child - it's
// swapped out for the real children once loadChildrenFor() resolves.
function toNode(entry) {
  const node = {
    value: entry.dn,
    label: entry.name || entry.dn,
    icon: entry.is_group ? 'i-lucide-users' : 'i-lucide-network',
    isGroup: entry.is_group,
  }
  if (!entry.is_group) {
    node.childrenLoaded = false
    node.children = [{ value: `${entry.dn}::placeholder`, label: 'Loading…', disabled: true }]
  }
  return node
}

function findNode(nodes, value) {
  for (const node of nodes) {
    if (node.value === value) return node
    if (node.children) {
      const found = findNode(node.children, value)
      if (found) return found
    }
  }
  return null
}

function loadChildrenFor(node) {
  if (!node || node.childrenLoaded) return
  browseLdap(node.value)
    .then((entries) => {
      node.children = entries.map(toNode)
      node.childrenLoaded = true
    })
    .catch(() => {
      node.children = []
      node.childrenLoaded = true
    })
}

function loadRoot() {
  loading.value = true
  error.value = null
  items.value = []
  browseLdap('')
    .then((entries) => {
      items.value = entries.map(toNode)
    })
    .catch((err) => {
      error.value = err.response?.data?.error ?? 'Failed to browse the directory.'
    })
    .finally(() => {
      loading.value = false
    })
}

function selectAndClose(node) {
  if (!node.isGroup) return
  emit('select', node.value)
  close()
}

function confirmSelect() {
  if (!selectedNode.value?.isGroup) return
  emit('select', selectedNode.value.value)
  close()
}

function close() {
  emit('update:visible', false)
}

watch(expandedKeys, (keys, oldKeys) => {
  const previouslyExpanded = new Set(oldKeys ?? [])
  for (const key of keys) {
    if (!previouslyExpanded.has(key)) {
      loadChildrenFor(findNode(items.value, key))
    }
  }
})

watch(
  () => props.visible,
  (visible) => {
    if (visible) {
      selectedNode.value = null
      expandedKeys.value = []
      loadRoot()
    }
  },
)
</script>

<template>
  <UModal
    :open="visible"
    title="Browse directory"
    :ui="{ content: 'sm:max-w-xl' }"
    @update:open="(v) => emit('update:visible', v)"
  >
    <template #body>
      <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-3" />
      <UTree
        v-model="selectedNode"
        v-model:expanded="expandedKeys"
        :items="items"
        :get-key="(item) => item.value"
        :loading="loading"
        class="max-h-80 overflow-y-auto"
      >
        <template #item-label="{ item }">
          <span @dblclick="selectAndClose(item)">{{ item.label }}</span>
        </template>
      </UTree>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
      <UButton label="Select" icon="i-lucide-check" :disabled="!selectedNode?.isGroup" @click="confirmSelect" />
    </template>
  </UModal>
</template>
