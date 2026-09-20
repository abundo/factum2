<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, nextTick, ref, watch } from 'vue'
import { listDhcpLeases } from '@/api/dns'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'

const open = defineModel('open', { type: Boolean, default: false })
const emit = defineEmits(['select'])

const toast = useToast()
const leases = ref([])
const loading = ref(false)
const filter = ref('')
const selectedKey = ref('')
const table = ref(null)
const sorting = ref([{ id: 'ip', desc: false }])

const pickerTableUi = {
  th: 'px-3 py-2',
  td: 'px-3 py-2 font-mono text-sm',
  tbody: '[&>tr[data-selected=true]]:bg-primary/25 [&>tr[data-selected=true]]:hover:bg-primary/30',
  tr: [
    'data-[selected=true]:bg-primary/25',
    'data-[selected=true]:hover:bg-primary/30',
    'data-[selected=true]:shadow-[inset_3px_0_0_0_var(--ui-primary)]',
    '[&[data-selected=true]>td]:text-highlighted',
    '[&[data-selected=true]>td]:font-medium',
  ].join(' '),
}

const columns = [
  { accessorKey: 'mac', header: 'MAC' },
  { accessorKey: 'ip', header: 'IP' },
  { accessorKey: 'hostname', header: 'Hostname' },
  { accessorKey: 'family', header: 'Family' },
]

const rowSelection = computed(() => (selectedKey.value ? { [selectedKey.value]: true } : {}))

function leaseKey(row) {
  return `${row.family}:${row.ip}:${row.mac}`
}

function familyLabel(family) {
  if (family === 'ipv6') return 'IPv6'
  return 'IPv4'
}

const listedLeases = computed(() =>
  (leases.value || []).map((row) => ({
    ...row,
    familyLabel: familyLabel(row.family),
  })),
)

function onSelect(_e, row) {
  selectedKey.value = leaseKey(row.original)
}

function selectedLease() {
  return listedLeases.value.find((row) => leaseKey(row) === selectedKey.value) || null
}

function confirmSelection() {
  const lease = selectedLease()
  if (!lease?.mac) return
  emit('select', {
    mac: lease.mac,
    ip: lease.ip,
    hostname: lease.hostname,
    family: lease.family,
  })
  open.value = false
}

function loadLeases() {
  loading.value = true
  listDhcpLeases()
    .then((data) => {
      leases.value = data ?? []
      if (selectedKey.value && !leases.value.some((row) => leaseKey(row) === selectedKey.value)) {
        selectedKey.value = ''
      }
    })
    .catch((err) => {
      leases.value = []
      toast.add({
        color: 'error',
        title: 'Could not load DHCP leases',
        description: err.response?.data?.error || err.message || 'Failed to load leases.',
        duration: 4000,
      })
    })
    .finally(() => {
      loading.value = false
    })
}

watch(open, (isOpen) => {
  if (!isOpen) return
  filter.value = ''
  selectedKey.value = ''
  loadLeases()
})

watch(
  [loading, selectedKey, listedLeases],
  () => {
    if (!loading.value && selectedKey.value) {
      nextTick(() => {
        const root = table.value?.$el ?? table.value
        root?.querySelector?.('[data-selected="true"]')?.scrollIntoView({ block: 'nearest' })
      })
    }
  },
  { flush: 'post' },
)
</script>

<template>
  <UModal v-model:open="open" title="Select DHCP lease" :ui="{ content: 'sm:max-w-4xl' }">
    <template #body>
      <div class="flex flex-col gap-2 min-w-0">
        <div class="flex items-center justify-between gap-2">
          <p class="text-muted text-sm">
            Current IPv4 and IPv6 leases from Kea. Selecting a row copies its MAC onto the DNS
            record.
          </p>
          <SearchInput v-model="filter" size="sm" class="w-56 shrink-0" />
        </div>
        <UTable
          ref="table"
          v-model:sorting="sorting"
          v-model:global-filter="filter"
          :data="listedLeases"
          :columns="columns"
          :loading="loading"
          :row-selection="rowSelection"
          :get-row-id="leaseKey"
          empty="No DHCP leases with a MAC address."
          sticky
          class="max-h-[50vh]"
          :ui="pickerTableUi"
          @select="onSelect"
        >
          <template
            v-for="col in columns"
            :key="col.accessorKey"
            #[`${col.accessorKey}-header`]="{ column }"
          >
            <SortableColumnHeader :column="column" :label="col.header" />
          </template>
          <template #mac-cell="{ row }">
            <span class="inline-flex items-center gap-2">
              <UIcon
                v-if="row.getIsSelected()"
                name="i-lucide-check"
                class="size-4 text-primary shrink-0"
              />
              <span v-else class="size-4 shrink-0" />
              {{ row.original.mac }}
            </span>
          </template>
          <template #family-cell="{ row }">
            {{ row.original.familyLabel }}
          </template>
        </UTable>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="open = false" />
      <UButton
        label="Select"
        icon="i-lucide-check"
        :disabled="!selectedLease()?.mac"
        @click="confirmSelection"
      />
    </template>
  </UModal>
</template>
