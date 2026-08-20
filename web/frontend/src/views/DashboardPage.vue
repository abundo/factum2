<script setup>
import { computed, onMounted, ref } from 'vue'
import { getLinks } from '@/api/links'

const links = ref([])

const groups = computed(() => {
  const map = new Map()
  for (const l of links.value) {
    if (!map.has(l.group)) map.set(l.group, [])
    map.get(l.group).push(l)
  }
  return [...map.entries()].map(([name, items]) => ({ name, items }))
})

onMounted(() => {
  getLinks()
    .then((data) => {
      links.value = data ?? []
    })
    .catch(() => {})
})
</script>

<template>
  <div class="grid grid-cols-12 gap-8">
    <div class="col-span-12">
      <div class="card">
        <div class="font-semibold text-xl mb-4">Dashboard</div>
        <p class="text-muted-color">Welcome to Factum.</p>
      </div>
    </div>
    <div
      v-for="group in groups"
      :key="group.name"
      class="col-span-12 md:col-span-6 xl:col-span-4"
    >
      <div class="card">
        <div class="font-semibold text-lg mb-4">{{ group.name }}</div>
        <div class="flex flex-col gap-1">
          <a
            v-for="l in group.items"
            :key="l.id"
            :href="l.url"
            :target="l.open_in_new_tab ? '_blank' : '_self'"
            rel="noopener noreferrer"
            class="flex items-center gap-3 p-2 rounded-lg hover:bg-elevated transition-colors"
          >
            <img v-if="l.icon" :src="l.icon" class="size-15 rounded object-contain shrink-0" />
            <UIcon v-else name="i-lucide-link" class="size-15 text-muted-color shrink-0" />
            <span class="truncate">{{ l.name }}</span>
          </a>
        </div>
      </div>
    </div>
  </div>
</template>
