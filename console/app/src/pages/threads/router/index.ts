/**
 * Thread module route definitions.
 *
 * Defines the nested route structure under `/threads`:
 * - `/threads` (ThreadsHome) - Landing page with suggestion chips.
 * - `/threads/search` (ThreadSearch) - Thread search/filter view.
 * - `/threads/:threadId` (Thread) - Individual thread conversation view.
 *
 * The root `/` path redirects to `/threads`.
 * All route components are lazy-loaded for code splitting.
 *
 * @module pages/threads/router
 */
import type { RouteRecordRaw } from 'vue-router'

/** Thread module route definitions, consumed by the root router. */
const routes: readonly RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/threads',
  },
  {
    path: '/threads',
    name: 'ThreadsRoot',
    component: () => import('../ThreadsLayout.vue'),
    children: [
      {
        path: 'search',
        name: 'ThreadSearch',
        component: () => import('../ThreadSearchView.vue'),
      },
      {
        path: '',
        name: 'ThreadsHome',
        component: () => import('../ThreadsLandingView.vue'),
      },
      {
        path: ':threadId',
        name: 'Thread',
        component: () => import('../ThreadView.vue'),
      },
    ],
  },
]

export default routes
