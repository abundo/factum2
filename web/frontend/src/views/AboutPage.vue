<script setup>
import { computed, ref } from 'vue'
import { useVersion } from '@/composables/useVersion'
import { formatDateTime } from '@/utils/datetime'

const GITHUB_URL = 'https://github.com/abundo/factum2'

const { info } = useVersion()
const copied = ref(false)

const displayDate = computed(() => {
  const d = info.value?.date
  if (!d || d === 'unknown') return null
  const parsed = new Date(d)
  if (Number.isNaN(parsed.getTime())) return d
  return formatDateTime(d)
})

const commit = computed(() => {
  const c = info.value?.commit
  if (!c || c === 'none') return null
  return c
})

const commitUrl = computed(() => (commit.value ? `${GITHUB_URL}/commit/${commit.value}` : null))

function copyCommit() {
  if (!commit.value || !navigator.clipboard) return
  navigator.clipboard.writeText(commit.value).then(() => {
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 1500)
  })
}
</script>

<template>
  <div class="card max-w-2xl">
    <div class="font-semibold text-xl mb-2">About Factum</div>
    <p class="text-muted-color mb-6">
      Factum tracks network infrastructure (devices, customers, services) and syncs it with external
      systems of record. NetBox and Lime CRM sync data into Factum; DNS, Icinga, LibreNMS, Oxidized
      and Prometheus are synced from Factum.
    </p>

    <dl v-if="info" class="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-3 text-sm items-center">
      <dt class="text-muted">Version</dt>
      <dd class="flex items-center gap-2">
        <span class="font-medium">{{ info.version }}</span>
        <UBadge v-if="info.dirty" color="warning" variant="subtle" size="xs">dirty</UBadge>
      </dd>

      <dt class="text-muted">Commit</dt>
      <dd>
        <div v-if="commit" class="flex items-center gap-1">
          <a
            :href="commitUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="font-mono hover:underline"
            :title="commit"
          >
            {{ commit.slice(0, 12) }}
          </a>
          <UButton
            :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            :title="copied ? 'Copied' : 'Copy commit hash'"
            :aria-label="copied ? 'Copied' : 'Copy commit hash'"
            @click="copyCommit"
          />
        </div>
        <span v-else class="text-muted">unknown</span>
      </dd>

      <dt class="text-muted">Built</dt>
      <dd>{{ displayDate || 'unknown' }}</dd>

      <dt class="text-muted">Go</dt>
      <dd class="font-mono">{{ info.go_version || 'unknown' }}</dd>

      <dt class="text-muted">License</dt>
      <dd>
        <a
          href="https://www.gnu.org/licenses/agpl-3.0.html"
          target="_blank"
          rel="noopener noreferrer"
          class="hover:underline"
        >
          GNU Affero General Public License v3.0
        </a>
      </dd>

      <dt class="text-muted">Source</dt>
      <dd>
        <UButton
          label="GitHub"
          icon="i-lucide-github"
          variant="outline"
          color="neutral"
          size="sm"
          :href="GITHUB_URL"
          target="_blank"
          external
        />
      </dd>
    </dl>
  </div>
</template>
