/**
 * Vue Router configuration.
 *
 * Defines top-level routes for the console application:
 * - `/threads` - Chat thread views (landing, search, individual thread).
 * - `/automation` - Scheduled automation management.
 *
 * Uses HTML5 history mode with lazy-loaded route components for code splitting.
 *
 * @module plugins/router
 */
import threadsRoutes from '@/pages/threads/router'
import type { RouteRecordRaw } from 'vue-router'
import { createRouter, createWebHistory } from 'vue-router'

/** Route definition for the automation management page. */
const automationRoute: RouteRecordRaw = {
  path: '/automation',
  name: 'Automations',
  component: () => import('@/pages/automation/AutomationView.vue'),
}

/**
 * The application router instance.
 * Thread routes are spread first (including the `/` → `/threads` redirect),
 * followed by the automation route.
 */
const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [...threadsRoutes, automationRoute],
})

export default router
