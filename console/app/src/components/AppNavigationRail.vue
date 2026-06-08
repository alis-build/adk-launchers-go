<script setup lang="ts">
  import AppSettingsMenu from '@/components/AppSettingsMenu.vue'
  import { clearAgentInstance } from '@/pages/threads/composables/useChatStream'
  import { deleteThread as deleteThreadREST, updateUserThreadState } from '@/pages/threads/clients'
  import { clearMessages } from '@/pages/threads/store/messages'
  import { getLoadingLoadThreads, getThreadHasUnread, getThreads, isThreadTitleLoading, loadThreads, removeThread, updateThread, type ThreadListItem } from '@/pages/threads/store/threads'
  import { getContextIdFromThreadName } from '@/pages/threads/utils'
  import { getShellLogoUrl } from '@/runtimeConfig'
  import { useAppStore } from '@/store/app'
  import { useSelectedAgentStore } from '@/store/selectedAgent'
  import { useSnackbarStore } from '@/store/snackbar'
  import { computed, onMounted, ref, watch } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { useDisplay, useTheme } from 'vuetify'

  const route = useRoute()
  const router = useRouter()
  const app = useAppStore()
  const agentStore = useSelectedAgentStore()
  const snackbarStore = useSnackbarStore()
  const { mdAndUp } = useDisplay()
  const theme = useTheme()
  const agentIconSrc = computed(() => getShellLogoUrl())
  const shellTitle = computed(() => agentStore.shellTitle)
  const showAgentSelector = computed(() => agentStore.showAgentSelector)

  const threads = computed(() =>
    [...getThreads()].sort((left, right) => {
      if (left.pinned !== right.pinned) {
        return left.pinned ? -1 : 1
      }

      const pinnedTimeDelta = timestampMs(right.pinnedTime) - timestampMs(left.pinnedTime)
      if (pinnedTimeDelta !== 0) {
        return pinnedTimeDelta
      }

      return timestampMs(right.createTime) - timestampMs(left.createTime)
    }),
  )
  const threadsLoading = getLoadingLoadThreads()
  const deletingThreadNames = ref(new Set<string>())
  const pinningThreadNames = ref(new Set<string>())

  const drawerWidth = computed(() => {
    if (!mdAndUp.value) return 308
    return app.navigationDrawerOpen ? 280 : 76
  })
  const threadsActive = computed(() => {
    const name = route.name?.toString() ?? ''
    return ['ThreadsHome', 'Thread', 'ThreadsRoot'].includes(name)
  })
  const searchActive = computed(() => route.name === 'ThreadSearch')
  const automationsActive = computed(() => route.name === 'Automations')
  const drawerVisible = computed(() => (mdAndUp.value ? true : app.navigationDrawerOpen))
  const showExpandedContent = computed(() => !mdAndUp.value || app.navigationDrawerOpen)
  const isLightTheme = computed(() => !theme.global.current.value.dark)

  const activeThreadId = computed(() => (route.params.threadId as string | undefined) ?? '')

  /**
   * Checks if the given thread is the one currently being viewed.
   * @param threadName - The thread resource name to check.
   */
  function isThreadActive(threadName: string): boolean {
    return getContextIdFromThreadName(threadName) === activeThreadId.value
  }

  onMounted(() => {
    void loadThreads()
  })

  watch(
    () => agentStore.selectedAgentId,
    () => {
      void loadThreads()
    },
  )

  /** Closes the drawer on mobile after a navigation action. */
  function closeMobileDrawer(): void {
    if (!mdAndUp.value) {
      app.closeNavigationDrawer()
    }
  }

  /** Syncs the global agent selector to the thread's bound agent on navigation. */
  function onThreadClick(thread: ThreadListItem): void {
    if (thread.agentId) {
      agentStore.setSelectedAgentId(thread.agentId)
    }
    closeMobileDrawer()
  }

  /**
   * Converts a protobuf Timestamp to milliseconds since epoch for sorting.
   * @param timestamp - A protobuf-style timestamp with `seconds` and optional `nanos`.
   */
  function timestampMs(timestamp?: { seconds?: number | bigint; nanos?: number }): number {
    if (!timestamp) return 0
    return Number(timestamp.seconds ?? 0) * 1000 + Math.floor((timestamp.nanos ?? 0) / 1_000_000)
  }

  /**
   * Toggles the pinned state of a thread via the ThreadService API.
   * Optimistically updates the local thread list on success.
   *
   * @param thread - The thread to pin or unpin.
   */
  async function handleTogglePin(thread: ThreadListItem): Promise<void> {
    if (!thread.name || pinningThreadNames.value.has(thread.name)) return

    pinningThreadNames.value = new Set(pinningThreadNames.value).add(thread.name)

    try {
      const userThreadState = await updateUserThreadState({
        userThreadState: {
          thread: thread.name,
          pinned: !thread.pinned,
        },
        updateMask: 'pinned',
      })

      updateThread({
        ...thread,
        pinned: userThreadState.pinned,
        pinnedTime: userThreadState.pinnedTime,
        readRunCount: Number(userThreadState.readRunCount),
      })

      snackbarStore.success(userThreadState.pinned ? 'Thread pinned.' : 'Thread unpinned.')
    } catch (e) {
      const message = (e as Error).message ?? `Failed to ${thread.pinned ? 'unpin' : 'pin'} thread`
      snackbarStore.error(message, { timeout: message.length > 160 ? 20000 : 8000 })
    } finally {
      const next = new Set(pinningThreadNames.value)
      next.delete(thread.name)
      pinningThreadNames.value = next
    }
  }

  /**
   * Deletes a thread via the ThreadService API.
   * Removes it from the local list and navigates away if it was active.
   *
   * @param threadName - The resource name of the thread to delete.
   */
  async function handleDeleteThread(threadName: string): Promise<void> {
    if (!threadName || deletingThreadNames.value.has(threadName)) return

    deletingThreadNames.value = new Set(deletingThreadNames.value).add(threadName)

    try {
      const contextId = getContextIdFromThreadName(threadName)
      await deleteThreadREST(contextId)
      clearMessages(contextId)
      clearAgentInstance(contextId)
      removeThread(threadName)

      if (isThreadActive(threadName)) {
        await router.push({ name: 'ThreadsHome' })
      }

      snackbarStore.success('Thread deleted.')
    } catch (e) {
      const message = (e as Error).message ?? 'Failed to delete thread'
      snackbarStore.error(message, { timeout: message.length > 160 ? 20000 : 8000 })
    } finally {
      const next = new Set(deletingThreadNames.value)
      next.delete(threadName)
      deletingThreadNames.value = next
    }
  }
</script>

<template>
  <v-navigation-drawer
    :model-value="drawerVisible"
    @update:model-value="
      (value) => {
        if (!mdAndUp) app.setNavigationDrawerOpen(value)
      }
    "
    :width="drawerWidth"
    :permanent="mdAndUp"
    :temporary="!mdAndUp"
    floating
    location="left"
    class="app-navigation-drawer"
    :class="{ 'app-navigation-drawer--light': isLightTheme }"
  >
    <div class="d-flex flex-column h-100">
      <div class="drawer-top px-3 pt-4 pb-2">
        <div
          v-if="showExpandedContent"
          class="drawer-brand d-flex align-center mb-4 px-3"
        >
          <v-avatar
            size="40"
            class="mr-3"
          >
            <v-img
              :src="agentIconSrc"
              cover
            />
          </v-avatar>
          <div class="min-w-0 flex-grow-1">
            <div class="text-title-medium font-weight-medium text-truncate">{{ shellTitle }}</div>
            <v-select
              v-if="showAgentSelector"
              v-model="agentStore.selectedAgentId"
              :items="agentStore.agents"
              density="compact"
              hide-details
              variant="outlined"
              class="agent-select mt-1"
              aria-label="Select agent"
            />
            <div
              v-else-if="agentStore.selectedAgentId"
              class="text-body-small text-medium-emphasis text-truncate"
            >
              {{ agentStore.selectedAgentId }}
            </div>
            <div class="text-body-small text-medium-emphasis text-truncate">{{ app.userDisplayName }}</div>
          </div>
        </div>

        <div
          class="d-flex"
          :class="showExpandedContent ? 'justify-space-between' : 'justify-center'"
        >
          <v-btn
            icon
            variant="text"
            class="drawer-icon-btn"
            :aria-label="showExpandedContent ? (mdAndUp ? 'Collapse menu' : 'Close menu') : 'Expand menu'"
            @click="app.toggleNavigationDrawer"
          >
            <v-icon size="22">menu</v-icon>
            <v-tooltip activator="parent">
              {{ showExpandedContent ? (mdAndUp ? 'Collapse menu' : 'Close menu') : 'Expand menu' }}
            </v-tooltip>
          </v-btn>

          <v-btn
            v-if="showExpandedContent"
            icon
            variant="text"
            class="drawer-icon-btn"
            aria-label="Search threads"
            :to="{ name: 'ThreadSearch' }"
            :active="searchActive"
            @click="closeMobileDrawer"
          >
            <v-icon size="18">search</v-icon>
            <v-tooltip activator="parent">Search threads</v-tooltip>
          </v-btn>
        </div>

        <div
          v-if="!showExpandedContent"
          class="mt-4 d-flex justify-center"
        >
          <v-btn
            icon
            variant="text"
            class="drawer-icon-btn"
            :to="{ name: 'ThreadSearch' }"
            :active="searchActive"
            aria-label="Search threads"
            @click="closeMobileDrawer"
          >
            <v-icon size="20">search</v-icon>
            <v-tooltip activator="parent">Search threads</v-tooltip>
          </v-btn>
        </div>

        <div class="mt-4">
          <v-btn
            v-if="showExpandedContent"
            block
            variant="text"
            rounded="pill"
            class="justify-start text-none drawer-action-btn"
            :to="{ name: 'ThreadsHome' }"
            :active="threadsActive && !activeThreadId"
            @click="closeMobileDrawer"
          >
            <v-icon class="mr-3">edit_square</v-icon>
            New thread
          </v-btn>
          <v-btn
            v-else
            icon
            variant="text"
            class="drawer-icon-btn mx-auto"
            :to="{ name: 'ThreadsHome' }"
            aria-label="New thread"
            @click="closeMobileDrawer"
          >
            <v-icon size="20">edit_square</v-icon>
            <v-tooltip activator="parent">New thread</v-tooltip>
          </v-btn>
        </div>

        <div class="mt-2">
          <v-btn
            v-if="showExpandedContent"
            block
            variant="text"
            rounded="pill"
            class="justify-start text-none drawer-action-btn"
            :to="{ name: 'Automations' }"
            :active="automationsActive"
            @click="closeMobileDrawer"
          >
            <v-icon class="mr-3">history_toggle_off</v-icon>
            Automations
          </v-btn>
          <v-btn
            v-else
            icon
            variant="text"
            class="drawer-icon-btn mx-auto"
            :to="{ name: 'Automations' }"
            aria-label="Automations"
            @click="closeMobileDrawer"
          >
            <v-icon size="20">history_toggle_off</v-icon>
            <v-tooltip activator="parent">Automations</v-tooltip>
          </v-btn>
        </div>
      </div>

      <div
        v-if="showExpandedContent"
        class="drawer-section px-3 pb-3"
      >
        <div class="px-3 mt-4 mb-2 text-label-large">Recent Threads</div>
        <div class="drawer-scroll">
          <v-skeleton-loader
            v-if="threadsLoading"
            type="list-item@7"
            color="transparent"
          />

          <v-list
            v-else
            nav
            bg-color="transparent"
            class="py-0"
            density="compact"
          >
            <v-list-item
              v-for="thread in threads"
              :key="thread.name"
              :to="{ name: 'Thread', params: { threadId: getContextIdFromThreadName(thread.name) } }"
              rounded="xl"
              :active="isThreadActive(thread.name)"
              color="primary"
              class="drawer-thread-item"
              :class="{ 'drawer-thread-item--loading': isThreadTitleLoading(thread.name) }"
              ripple
              @click="onThreadClick(thread)"
            >
              <v-list-item-title
                v-if="thread.displayName"
                class="text-body-medium text-truncate text-left ml-1"
              >
                {{ thread.displayName }}
              </v-list-item-title>

              <div
                v-else-if="isThreadTitleLoading(thread.name)"
                class="thread-title-loading"
              ></div>

              <template #append>
                <div
                  class="drawer-thread-menu-wrap"
                  @click.stop.prevent
                >
                  <v-icon
                    v-if="thread.pinned"
                    size="16"
                    class="drawer-thread-pin-icon mr-1"
                  >
                    keep
                  </v-icon>
                  <v-badge
                    v-if="getThreadHasUnread(thread)"
                    dot
                    color="success"
                    inline
                    class="drawer-thread-unread-dot"
                  />
                  <v-menu
                    location="end center"
                    origin="start center"
                    offset="0"
                    content-class="drawer-thread-menu-content"
                  >
                    <template #activator="{ props: menuProps }">
                      <v-btn
                        icon
                        size="x-small"
                        variant="text"
                        class="drawer-thread-menu-btn"
                        v-bind="menuProps"
                        :loading="deletingThreadNames.has(thread.name) || pinningThreadNames.has(thread.name)"
                        aria-label="Thread menu"
                        @click.stop.prevent
                        @mousedown.stop.prevent
                      >
                        <v-icon size="18">more_vert</v-icon>
                      </v-btn>
                    </template>

                    <v-list
                      density="compact"
                      min-width="0"
                      class="drawer-thread-menu-list"
                    >
                      <v-list-item
                        rounded="lg"
                        class="drawer-thread-menu-item"
                        :disabled="deletingThreadNames.has(thread.name) || pinningThreadNames.has(thread.name)"
                        @click.stop="handleTogglePin(thread)"
                      >
                        <template #prepend>
                          <v-icon>{{ thread.pinned ? 'keep_off' : 'keep' }}</v-icon>
                        </template>
                        <v-list-item-title>{{ thread.pinned ? 'Unpin thread' : 'Pin thread' }}</v-list-item-title>
                      </v-list-item>
                      <v-list-item
                        rounded="lg"
                        class="drawer-thread-menu-item"
                        :disabled="deletingThreadNames.has(thread.name) || pinningThreadNames.has(thread.name)"
                        @click.stop="handleDeleteThread(thread.name)"
                      >
                        <template #prepend>
                          <v-icon>delete</v-icon>
                        </template>
                        <v-list-item-title>Delete thread</v-list-item-title>
                      </v-list-item>
                    </v-list>
                  </v-menu>
                </div>
              </template>
            </v-list-item>

            <v-list-item
              v-if="!threads.length"
              rounded="xl"
              class="is-empty"
            >
              <v-list-item-title class="text-body-medium text-medium-emphasis"> No threads yet </v-list-item-title>
            </v-list-item>
          </v-list>
        </div>
      </div>

      <div class="mt-auto pt-7 px-3 pb-4">
        <app-settings-menu :expanded="showExpandedContent" />
      </div>
    </div>
  </v-navigation-drawer>
</template>

<style scoped lang="scss">
  .app-navigation-drawer {
    background: rgb(var(--v-theme-sidebar)) !important;
    // border-right: 1px solid rgb(var(--v-theme-sidebar-border));
  }

  .drawer-icon-btn,
  .drawer-action-btn {
    color: rgb(var(--v-theme-sidebar-text));
  }

  .drawer-brand {
    min-width: 0;
    color: rgb(var(--v-theme-sidebar-text));
  }

  .drawer-top {
    border-bottom: 1px solid rgba(var(--v-theme-sidebar-text), 0.06);
  }

  .drawer-icon-btn {
    border-radius: 999px;
  }

  .drawer-action-btn {
    min-height: 40px;
    padding-inline: 0.9rem;
    background: rgba(var(--v-theme-sidebar-text), 0.05);
  }

  .drawer-action-btn.v-btn--active {
    background: rgb(var(--v-theme-sidebar-active));
  }

  .drawer-icon-btn.v-btn--active {
    background: rgba(var(--v-theme-sidebar-text), 0.1);
  }

  .drawer-section {
    position: relative;
    min-height: 0;
    overflow: hidden;
  }

  .drawer-section::before,
  .drawer-section::after {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    height: 2rem;
    pointer-events: none;
    z-index: 1;
  }

  .drawer-section::before {
    top: 0;
    background: linear-gradient(to bottom, rgb(var(--v-theme-sidebar)) 0%, rgba(0, 0, 0, 0) 100%);
  }

  .drawer-section::after {
    bottom: 0;
    background: linear-gradient(to top, rgb(var(--v-theme-sidebar)) 0%, rgba(0, 0, 0, 0) 100%);
  }

  @media (max-width: 959px) {
    .app-navigation-drawer {
      box-shadow: 0 18px 44px rgba(0, 0, 0, 0.22) !important;
    }
  }

  .drawer-scroll {
    height: 100%;
    overflow-y: auto;
    padding-top: 0.25rem;
    padding-bottom: 0.25rem;
  }

  .thread-title-loading {
    width: 100%;
    height: 1.1rem;
  }

  .drawer-thread-pin-icon {
    color: rgba(var(--v-theme-sidebar-text), 0.72);
    transform: rotate(35deg);
  }

  .drawer-list {
    color: inherit;
  }

  .drawer-thread-item:hover {
    background: rgb(var(--v-theme-sidebar-hover));
  }

  .drawer-thread-menu-wrap {
    display: flex;
    align-items: center;
    gap: 0.125rem;
    margin-left: 0.25rem;
  }

  .drawer-thread-unread-dot {
    flex: 0 0 auto;
  }

  .drawer-thread-unread-dot :deep(.v-badge__badge) {
    width: 9px;
    height: 9px;
    min-width: 9px;
    border-radius: 999px;
    box-shadow: 0 0 0 2px rgb(var(--v-theme-sidebar));
  }

  .drawer-thread-menu-btn {
    color: rgb(var(--v-theme-sidebar-text));
    width: 28px;
    height: 28px;
    opacity: 0;
    border-radius: 999px;
    background: transparent;
  }

  .drawer-thread-item:hover .drawer-thread-menu-btn,
  .drawer-thread-item.v-list-item--active .drawer-thread-menu-btn {
    opacity: 1;
  }

  .drawer-thread-menu-btn:hover {
    background: rgba(var(--v-theme-sidebar-text), 0.08);
  }

  .drawer-thread-item.v-list-item--active {
    background: rgb(var(--v-theme-sidebar-active));
  }

  .drawer-thread-item--loading {
    position: relative;
    overflow: hidden;
    background: rgba(var(--v-theme-sidebar-text), 0.05);
    animation: drawer-thread-loading-pulse 1.8s ease-in-out infinite;
  }

  .drawer-thread-item--loading::after {
    content: '';
    position: absolute;
    inset: 0;
    transform: translateX(-100%);
    pointer-events: none;
    background: linear-gradient(
      90deg,
      rgba(var(--v-theme-sidebar-text), 0) 0%,
      rgba(var(--v-theme-sidebar-text), 0.06) 42%,
      rgba(var(--v-theme-sidebar-text), 0.14) 50%,
      rgba(var(--v-theme-sidebar-text), 0.06) 58%,
      rgba(var(--v-theme-sidebar-text), 0) 100%
    );
    animation: drawer-thread-loading-shimmer 1.8s ease-in-out infinite;
  }

  .drawer-thread-item.is-empty {
    background: rgba(var(--v-theme-sidebar-text), 0.04);
  }

  .drawer-thread-menu-list {
    padding: 0.25rem;
    background: transparent;
    width: max-content;
  }

  .drawer-thread-menu-item {
    min-height: 36px;
  }

  @keyframes drawer-thread-loading-pulse {
    0%,
    100% {
      background: rgba(var(--v-theme-sidebar-text), 0.04);
    }

    50% {
      background: rgba(var(--v-theme-sidebar-text), 0.1);
    }
  }

  @keyframes drawer-thread-loading-shimmer {
    0% {
      transform: translateX(-100%);
    }

    100% {
      transform: translateX(100%);
    }
  }

  .app-navigation-drawer--light .drawer-icon-btn {
    color: #486fb3;
  }

  .app-navigation-drawer--light .drawer-section::before {
    background: linear-gradient(to bottom, rgba(238, 243, 251, 0.96) 0%, rgba(238, 243, 251, 0) 100%);
  }

  .app-navigation-drawer--light .drawer-section::after {
    background: linear-gradient(to top, rgba(238, 243, 251, 0.96) 0%, rgba(238, 243, 251, 0) 100%);
  }

  .app-navigation-drawer--light .drawer-top .drawer-icon-btn:first-child {
    border: 2px solid rgba(66, 133, 244, 0.9);
    background: rgba(255, 255, 255, 0.32);
  }

  .app-navigation-drawer--light .drawer-top .drawer-icon-btn:last-child {
    color: rgba(var(--v-theme-sidebar-text), 0.72);
  }

  .app-navigation-drawer--light .drawer-icon-btn.v-btn--active {
    color: #3358a8;
    background: rgba(255, 255, 255, 0.42);
  }

  .app-navigation-drawer--light .drawer-thread-item.v-list-item--active,
  .app-navigation-drawer--light .drawer-action-btn.v-btn--active {
    color: #3358a8;
  }
</style>

<style lang="scss">
  .drawer-thread-menu-content {
    margin-left: -0.35rem;
    margin-top: 0;
    border-radius: 16px;
    border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
    background: rgb(var(--v-theme-surface));
    box-shadow: 0 10px 24px rgba(0, 0, 0, 0.2);
    overflow: hidden;
  }
</style>
