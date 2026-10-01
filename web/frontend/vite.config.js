import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'
import ui from '@nuxt/ui/vite'

// https://vite.dev/config/
export default defineConfig(() => ({
  base: '/',
  plugins: [
    vue(),
    vueDevTools(),
    ui({
      // Bundle every icon referenced in src/ (and Nuxt UI's own defaults)
      // at build time. main.js disables Iconify's public API so a missed
      // icon cannot fetch api.iconify.design at runtime.
      icon: { clientBundle: { scan: true } },
      // GUI design rules: a form label sits beside its field on a wide
      // screen and above it on a narrow one. See src/utils/form.js.
      ui: {
        formField: {
          slots: {
            root: 'sm:grid sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-x-4',
            labelWrapper: 'flex content-center items-center justify-between gap-1 sm:pt-1.5',
            container: 'relative mt-1 sm:mt-0',
          },
        },
      },
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  css: {
    preprocessorOptions: {
      scss: {
        api: 'modern-compiler',
      },
    },
  },
  build: {
    // Built straight into the location web.go serves from.
    outDir: '../static/vue',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8090',
        changeOrigin: true,
        ws: true,
      },
    },
  },
}))
