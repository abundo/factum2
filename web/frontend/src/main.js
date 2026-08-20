import { createApp } from 'vue'
import { createPinia } from 'pinia'

import ui from '@nuxt/ui/vue-plugin'

import App from './App.vue'
import router from './router'
import { useAuthStore } from './stores/auth'

import '@/assets/tailwind.css'

const app = createApp(App)

app.use(createPinia())

// Resolve who's logged in (if anyone) before the router's first navigation
// guard runs, so it doesn't have to guess/flicker on a not-yet-loaded user.
// This must happen before app.use(router): installing the router kicks off
// its first navigation (and therefore the first beforeEach guard) right
// away, not on app.mount() - so if the router were installed first, the
// guard would always see the freshly-initialized "logged out" state on a
// hard reload, even with a perfectly valid session cookie.
await useAuthStore().fetchCurrentUser()

app.use(router)
app.use(ui)

app.mount('#app')
