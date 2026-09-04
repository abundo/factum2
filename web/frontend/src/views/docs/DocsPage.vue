<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getDoc, listDocs } from '@/api/docs'
import { renderDocMarkdown } from '@/utils/markdown'

defineOptions({ name: 'DocsPage' })

const route = useRoute()
const router = useRouter()

const pages = ref([])
const listError = ref(null)
const pageError = ref(null)
const loadingList = ref(true)
const loadingPage = ref(true)
const markdown = ref('')

const currentSlug = computed(() => route.params.slug || 'index')

const html = computed(() => renderDocMarkdown(markdown.value))

const navItems = computed(() =>
  pages.value.map((p) => ({
    label: p.title,
    to: `/doc/${p.slug}`,
    active: p.slug === currentSlug.value,
  })),
)

function loadList() {
  loadingList.value = true
  listError.value = null
  listDocs()
    .then((data) => {
      pages.value = data ?? []
    })
    .catch(() => {
      listError.value = 'Failed to load documentation index.'
      pages.value = []
    })
    .finally(() => {
      loadingList.value = false
    })
}

function loadPage(slug) {
  loadingPage.value = true
  pageError.value = null
  markdown.value = ''
  getDoc(slug)
    .then((data) => {
      markdown.value = data?.markdown ?? ''
    })
    .catch((err) => {
      if (err.response?.status === 404) {
        pageError.value = 'This page is not in the documentation for this version.'
      } else {
        pageError.value = 'Failed to load this page.'
      }
    })
    .finally(() => {
      loadingPage.value = false
    })
}

function onDocClick(event) {
  const a = event.target.closest('a')
  if (!a) return
  const href = a.getAttribute('href')
  if (!href || !href.startsWith('/doc/')) return
  event.preventDefault()
  router.push(href)
}

onMounted(loadList)

watch(
  currentSlug,
  (slug) => {
    loadPage(slug)
  },
  { immediate: true },
)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-4 lg:flex-row lg:items-start">
    <nav class="lg:w-56 lg:shrink-0">
      <div v-if="loadingList" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-6 animate-spin" />
      </div>
      <UAlert v-else-if="listError" color="error" variant="subtle" :title="listError" />
      <div v-else class="flex flex-wrap gap-1 lg:flex-col">
        <UButton
          v-for="item in navItems"
          :key="item.to"
          :label="item.label"
          :to="item.to"
          :variant="item.active ? 'soft' : 'ghost'"
          color="neutral"
          class="justify-start"
        />
      </div>
    </nav>

    <article class="card min-w-0 flex-1">
      <div v-if="loadingPage" class="flex justify-center p-8">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>
      <UAlert v-else-if="pageError" color="error" variant="subtle" :title="pageError" />
      <!-- markdown is from the embedded docs/user files, sanitized in renderDocMarkdown -->
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div v-else class="doc-prose" v-html="html" @click="onDocClick" />
    </article>
  </div>
</template>

<style scoped>
.doc-prose {
  line-height: 1.65;
  overflow-x: auto;
}
.doc-prose :deep(h1) {
  font-size: 1.5rem;
  font-weight: 600;
  margin-bottom: 0.75rem;
}
.doc-prose :deep(h2) {
  font-size: 1.15rem;
  font-weight: 600;
  margin-top: 1.75rem;
  margin-bottom: 0.5rem;
}
.doc-prose :deep(h3) {
  font-size: 1rem;
  font-weight: 600;
  margin-top: 1.25rem;
  margin-bottom: 0.4rem;
}
.doc-prose :deep(p) {
  margin: 0.75rem 0;
}
.doc-prose :deep(a) {
  text-decoration: underline;
  text-underline-offset: 2px;
}
.doc-prose :deep(ul),
.doc-prose :deep(ol) {
  margin: 0.75rem 0;
  padding-left: 1.5rem;
}
.doc-prose :deep(ul) {
  list-style: disc;
}
.doc-prose :deep(ol) {
  list-style: decimal;
}
.doc-prose :deep(li) {
  margin: 0.25rem 0;
}
.doc-prose :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.875em;
  background: var(--ui-bg-elevated);
  padding: 0.1rem 0.35rem;
  border-radius: 0.25rem;
}
.doc-prose :deep(pre) {
  background: var(--ui-bg-elevated);
  padding: 1rem;
  border-radius: 0.5rem;
  overflow-x: auto;
  margin: 1rem 0;
  font-size: 0.875rem;
}
.doc-prose :deep(pre code) {
  background: transparent;
  padding: 0;
}
.doc-prose :deep(table) {
  width: 100%;
  font-size: 0.875rem;
  margin: 1rem 0;
  border-collapse: collapse;
}
.doc-prose :deep(th),
.doc-prose :deep(td) {
  text-align: left;
  padding: 0.5rem 0.75rem;
  border-bottom: 1px solid var(--ui-border);
  vertical-align: top;
}
.doc-prose :deep(th) {
  font-weight: 600;
}
.doc-prose :deep(blockquote) {
  border-left: 3px solid var(--ui-border);
  padding-left: 1rem;
  margin: 1rem 0;
  color: var(--ui-text-muted);
}
</style>
