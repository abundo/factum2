<script setup>
import { computed, ref } from 'vue'
import { useVersion } from '@/composables/useVersion'
import { formatDateTime } from '@/utils/datetime'

defineProps({
  compact: { type: Boolean, default: false },
})

const { info } = useVersion()
const copied = ref(false)

const shortCommit = computed(() => {
  const c = info.value?.commit
  if (!c || c === 'none') return null
  return c.slice(0, 12)
})

const displayDate = computed(() => {
  const d = info.value?.date
  if (!d || d === 'unknown') return null
  const parsed = new Date(d)
  if (Number.isNaN(parsed.getTime())) return d
  return formatDateTime(d)
})

function copyCommit() {
  const c = info.value?.commit
  if (!c || c === 'none' || !navigator.clipboard) return
  navigator.clipboard.writeText(c).then(() => {
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 1500)
  })
}
</script>

<template>
  <div v-if="info" class="text-xs text-muted">
    <div v-if="compact" class="flex flex-wrap items-center justify-center gap-x-2 gap-y-1">
      <span class="font-medium text-default">{{ info.version }}</span>
      <UBadge v-if="info.dirty" color="warning" variant="subtle" size="xs">dirty</UBadge>
      <button
        v-if="shortCommit"
        type="button"
        class="font-mono hover:text-default"
        :title="copied ? 'Copied' : 'Copy commit hash'"
        @click="copyCommit"
      >
        {{ shortCommit }}
      </button>
      <span v-if="displayDate">{{ displayDate }}</span>
    </div>
    <div v-else class="flex flex-col gap-0.5">
      <div class="flex items-center gap-1.5">
        <span class="font-medium text-default">{{ info.version }}</span>
        <UBadge v-if="info.dirty" color="warning" variant="subtle" size="xs">dirty</UBadge>
      </div>
      <button
        v-if="shortCommit"
        type="button"
        class="w-fit font-mono hover:text-default"
        :title="copied ? 'Copied' : 'Copy commit hash'"
        @click="copyCommit"
      >
        {{ shortCommit }}
      </button>
      <span v-if="displayDate">{{ displayDate }}</span>
      <span v-if="info.go_version" class="font-mono">{{ info.go_version }}</span>
    </div>
  </div>
</template>
