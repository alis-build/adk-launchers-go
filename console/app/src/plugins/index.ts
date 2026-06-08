/**
 * Central plugin registration module.
 *
 * Installs all global Vue plugins in the correct order:
 * 1. **Vuetify** - Material Design 3 component library and theming.
 * 2. **Router** - Vue Router for SPA navigation.
 * 3. **Pinia** - State management.
 * 4. **VueSpinnersPlugin** - Loading spinner components.
 * 5. **A2UiVueRenderer** - Registers A2UI/Vuetify renderer glue from
 *    `@alis-build/a2ui-vuetify-renderer` (components used by `A2UISurface` / `A2UIProvider`).
 *
 * @module plugins
 */
import { A2UiVueRenderer } from '@alis-build/a2ui-vuetify-renderer'
import type { App } from 'vue'
import { VueSpinnersPlugin } from 'vue3-spinners'
import pinia from './pinia'
import router from './router'
import vuetify from './vuetify'

/**
 * Registers all global Vue plugins on the given app instance.
 * Called once during application bootstrap in `main.ts`.
 *
 * @param app - The Vue application instance to install plugins on.
 */
export function registerPlugins(app: App) {
  app.use(vuetify).use(router).use(pinia).use(VueSpinnersPlugin).use(A2UiVueRenderer)
}
