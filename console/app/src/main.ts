/**
 * Application entry point.
 *
 * Bootstraps the Vue 3 app by loading deploy-time runtime configuration,
 * registering global plugins (Vuetify, Pinia, Router, A2UI renderer),
 * and mounting the root `App.vue` component to `#app`.
 *
 * @module main
 */
import { registerPlugins } from '@/plugins'
import { getRuntimeConfig, loadRuntimeConfig } from '@/runtimeConfig'
import { useSelectedAgentStore } from '@/store/selectedAgent'
import { createApp } from 'vue'
import App from './App.vue'

function applyBranding(): void {
  const branding = getRuntimeConfig().branding
  const title = branding?.title || branding?.displayName
  if (title) {
    document.title = title
  }
  if (branding?.faviconUrl) {
    let link = document.querySelector<HTMLLinkElement>("link[rel='icon']")
    if (!link) {
      link = document.createElement('link')
      link.rel = 'icon'
      document.head.appendChild(link)
    }
    link.href = branding.faviconUrl
  }
}

// Styles
import '@fontsource/roboto/400.css'
import '@fontsource/roboto/400-italic.css'
import '@fontsource/roboto/500.css'
import '@fontsource/roboto/700.css'
import './assets/tiptap-styles.css'
/** Base styles for `@alis-build/a2ui-vuetify-renderer` (layout tokens used by A2UI surfaces). */
import '@alis-build/a2ui-vuetify-renderer/style.css'

/**
 * Initializes the application: loads runtime config, creates the Vue app,
 * registers all plugins, and mounts to the DOM.
 *
 * Runtime config must be loaded first because plugins (e.g. router guards,
 * store initializers) may depend on deploy-time values like `gcpProject`.
 */
async function bootstrap() {
  await loadRuntimeConfig()
  applyBranding()

  const app = createApp(App)
  registerPlugins(app)
  app.mount('#app')
  useSelectedAgentStore().ensureDefaultAgent()
}

void bootstrap()
