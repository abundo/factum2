<script setup>
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useLayout } from '@/layout/composables/layout'
import { useLogPanel } from '@/layout/composables/logPanel'
import AppLogPanel from './AppLogPanel.vue'
import AppMenu from './AppMenu.vue'
import AppSidebar from './AppSidebar.vue'
import AppTopbar from './AppTopbar.vue'
import BuildInfo from '@/components/BuildInfo.vue'

const { layoutState, closeMobileMenu } = useLayout()
const { state: logPanelState } = useLogPanel()
const route = useRoute()

watch(
  () => route.path,
  () => closeMobileMenu(),
)
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden">
    <AppTopbar />
    <div class="flex min-h-0 flex-1">
      <AppSidebar />
      <main class="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto p-4 md:p-6">
        <router-view v-slot="{ Component }">
          <div class="flex min-h-0 flex-1 flex-col">
            <keep-alive include="CustomerList">
              <component :is="Component" />
            </keep-alive>
          </div>
        </router-view>
      </main>
    </div>
    <AppLogPanel v-if="logPanelState.open" />
  </div>

  <USlideover v-model:open="layoutState.mobileMenuOpen" side="left" title="Menu">
    <template #body>
      <div class="flex h-full flex-col">
        <div class="min-h-0 flex-1 overflow-y-auto">
          <AppMenu />
        </div>
        <BuildInfo class="mt-3 shrink-0 border-t border-default pt-3" />
      </div>
    </template>
  </USlideover>
</template>
