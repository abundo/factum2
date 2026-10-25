<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
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
const articleEl = ref(null)

const currentSlug = computed(() => route.params.slug || 'index')

const html = computed(() => renderDocMarkdown(markdown.value))

// TOPICS groups the docs the way AppMenu.vue groups the pages. A doc that
// is not listed here goes under Other, in the order the API returned.
const TOPICS = [
  { label: 'General', children: ['index'] },
  { label: 'Provisioning', children: ['services', 'config'] },
  { label: 'Infrastructure', children: ['datacenter'] },
  { label: 'DCIM', children: ['devices'] },
  { label: 'Services', children: ['dns', 'certs'] },
  { label: 'Jobs', children: ['jobs'] },
  { label: 'Tools', children: ['software'] },
  { label: 'Admin', children: ['settings', 'directory', 'radius'] },
  { label: 'Help', children: ['sbom'] },
]

const topics = computed(() => {
  const bySlug = new Map(pages.value.map((p) => [p.slug, p]))
  const used = new Set()
  const build = (node) => {
    if (typeof node === 'string') {
      const page = bySlug.get(node)
      if (!page) return null
      used.add(node)
      return {
        label: page.title,
        to: `/doc/${page.slug}`,
        active: page.slug === currentSlug.value,
      }
    }
    const children = node.children.map(build).filter(Boolean)
    return children.length ? { label: node.label, defaultOpen: true, children } : null
  }
  const tree = TOPICS.map(build).filter(Boolean)
  const rest = pages.value.filter((p) => !used.has(p.slug))
  if (rest.length) {
    tree.push(build({ label: 'Other', children: rest.map((p) => p.slug) }))
  }
  return tree
})

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

// The article is the scroll container. A new page starts at the top of
// that column, not the window.
watch(loadingPage, async (loading) => {
  if (loading) return
  await nextTick()
  articleEl.value?.scrollTo(0, 0)
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-4 lg:flex-row">
    <nav class="card max-h-64 shrink-0 overflow-y-auto p-3 lg:max-h-none lg:w-56">
      <div class="mb-2 px-2.5 text-sm font-semibold">Topic</div>
      <div v-if="loadingList" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-6 animate-spin" />
      </div>
      <UAlert v-else-if="listError" color="error" variant="subtle" :title="listError" />
      <UNavigationMenu v-else :items="topics" orientation="vertical" class="w-full" />
    </nav>

    <article
      ref="articleEl"
      class="card doc min-h-0 min-w-0 max-w-4xl flex-1 overflow-y-auto"
      @click="onDocClick"
    >
      <div v-if="loadingPage" class="flex justify-center p-8">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>
      <UAlert v-else-if="pageError" color="error" variant="subtle" :title="pageError" />
      <!-- markdown is from the embedded docs/user files, sanitized in renderDocMarkdown -->
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div v-else v-html="html" />
    </article>
  </div>
</template>

<style scoped>
.doc {
  line-height: 1.65;
}
.doc :deep(h1) {
  font-size: 1.5rem;
  font-weight: 600;
  margin-bottom: 0.75rem;
}
.doc :deep(h2) {
  font-size: 1.15rem;
  font-weight: 600;
  margin-top: 1.75rem;
  margin-bottom: 0.5rem;
}
.doc :deep(h3) {
  font-size: 1rem;
  font-weight: 600;
  margin-top: 1.25rem;
  margin-bottom: 0.4rem;
}
.doc :deep(p) {
  margin: 0.75rem 0;
}
.doc :deep(a) {
  text-decoration: underline;
  text-underline-offset: 2px;
}
.doc :deep(ul),
.doc :deep(ol) {
  margin: 0.75rem 0;
  padding-left: 1.5rem;
}
.doc :deep(ul) {
  list-style: disc;
}
.doc :deep(ol) {
  list-style: decimal;
}
.doc :deep(li) {
  margin: 0.25rem 0;
}
.doc :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.875em;
  background: var(--ui-bg-elevated);
  padding: 0.1rem 0.35rem;
  border-radius: 0.25rem;
}
.doc :deep(pre) {
  background: var(--ui-bg-elevated);
  padding: 1rem;
  border-radius: 0.5rem;
  overflow-x: auto;
  margin: 1rem 0;
  font-size: 0.875rem;
}
.doc :deep(pre code) {
  background: transparent;
  padding: 0;
}
.doc :deep(table) {
  width: 100%;
  font-size: 0.875rem;
  margin: 1rem 0;
  border-collapse: collapse;
}
.doc :deep(th),
.doc :deep(td) {
  text-align: left;
  padding: 0.5rem 0.75rem;
  border-bottom: 1px solid var(--ui-border);
  vertical-align: top;
}
.doc :deep(th) {
  font-weight: 600;
}
.doc :deep(blockquote) {
  border-left: 3px solid var(--ui-border);
  padding-left: 1rem;
  margin: 1rem 0;
  color: var(--ui-text-muted);
}
</style>
