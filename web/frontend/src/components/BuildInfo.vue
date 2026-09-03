<script setup>
import { computed, ref } from 'vue'
import { useVersion } from '@/composables/useVersion'
import { formatDateTime } from '@/utils/datetime'

defineProps({
  compact: { type: Boolean, default: false },
})

const GITHUB_URL = 'https://github.com/abundo/factum2'

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
      <div class="-ml-1.5 mt-1 flex items-center">
        <UButton
          icon="i-lucide-github"
          variant="ghost"
          color="neutral"
          size="xs"
          square
          :href="GITHUB_URL"
          target="_blank"
          external
          title="GitHub repository"
          aria-label="GitHub repository"
        />
        <UButton
          icon="i-lucide-info"
          variant="ghost"
          color="neutral"
          size="xs"
          square
          to="/about"
          title="About Factum"
          aria-label="About Factum"
        />
      </div>
    </div>
  </div>
</template>
