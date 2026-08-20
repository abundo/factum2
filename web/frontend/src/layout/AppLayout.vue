<script setup>
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useLayout } from '@/layout/composables/layout'
import { useLogPanel } from '@/layout/composables/logPanel'
import AppLogPanel from './AppLogPanel.vue'
import AppMenu from './AppMenu.vue'
import AppSidebar from './AppSidebar.vue'
import AppTopbar from './AppTopbar.vue'

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
      <main
        class="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto p-4 md:p-6"
        :style="logPanelState.open ? { paddingBottom: logPanelState.height + 'px' } : null"
      >
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
      <AppMenu />
    </template>
  </USlideover>
</template>
