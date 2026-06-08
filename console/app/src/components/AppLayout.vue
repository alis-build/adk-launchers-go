<script setup lang="ts">
  import AppNavigationRail from '@/components/AppNavigationRail.vue'
  import ProfileMenu from '@/components/ProfileMenu.vue'
  import { useAppStore } from '@/store/app'
  import { useSelectedAgentStore } from '@/store/selectedAgent'
  import { computed, ref, watch } from 'vue'
  import { useDisplay, useTheme } from 'vuetify'

  const appStore = useAppStore()
  const agentStore = useSelectedAgentStore()
  const showAgentSelector = computed(() => agentStore.showAgentSelector)
  /** Whether the profile dropdown menu is currently open. */
  const menuOpen = ref(false)
  const { mdAndUp } = useDisplay()
  const theme = useTheme()
  const showDesktopBrand = computed(() => mdAndUp.value)
  const isDarkTheme = computed(() => theme.global.current.value.dark)

  watch(
    mdAndUp,
    (isDesktop) => {
      appStore.setNavigationDrawerOpen(isDesktop)
    },
    { immediate: true },
  )
</script>

<template>
  <v-app>
    <app-navigation-rail />

    <v-app-bar
      class="app-top-bar d-flex align-center pr-4"
      :class="{ 'app-top-bar--dark': isDarkTheme }"
      elevation="0"
    >
      <template #prepend>
        <v-btn
          v-if="!mdAndUp"
          icon
          variant="text"
          class="ml-2"
          aria-label="Open navigation menu"
          @click="appStore.openNavigationDrawer()"
        >
          <v-icon size="22">menu</v-icon>
        </v-btn>
        <div
          v-else-if="showDesktopBrand"
          class="app-brand d-flex align-center text-title-large font-weight-medium pl-3"
        ></div>
      </template>
      <template #default>
        <div class="app-bar-main w-100 ml-lg-4">
          <div
            id="app-bar-title"
            class="app-bar-title-slot"
          ></div>
          <div
            id="app-bar-search"
            class="app-bar-search-slot"
          ></div>
        </div>
      </template>

      <template #append>
        <v-select
          v-if="showAgentSelector"
          v-model="agentStore.selectedAgentId"
          :items="agentStore.agents"
          label="Agent"
          density="compact"
          hide-details
          variant="outlined"
          class="agent-select mr-2"
          style="max-width: 220px"
          aria-label="Select agent"
        />
        <div
          id="app-bar-actions"
          class="app-bar-actions-slot mr-2"
        ></div>
        <v-menu
          v-if="mdAndUp"
          v-model="menuOpen"
          location="bottom end"
          offset="12"
          :close-on-content-click="false"
        >
          <template #activator="{ props: menuProps }">
            <v-btn
              icon
              variant="text"
              v-bind="menuProps"
              aria-label="Account menu"
            >
              <v-avatar
                size="40"
                color="primary"
              >
                <v-img
                  v-if="appStore.userProfilePicture"
                  :src="appStore.userProfilePicture"
                  cover
                />
                <span
                  v-else
                  class="text-body-medium"
                >
                  {{ appStore.userInitials }}
                </span>
              </v-avatar>
            </v-btn>
          </template>

          <profile-menu @close="menuOpen = false" />
        </v-menu>
      </template>
    </v-app-bar>

    <v-progress-linear
      v-if="appStore.globalThreadLoading"
      indeterminate
      color="primary"
      height="4"
      rounded
      class="app-shell-loader"
    />

    <v-main class="app-main">
      <div class="app-main-content">
        <slot></slot>
      </div>
    </v-main>
  </v-app>
</template>

<style lang="scss">
  html,
  body,
  #app {
    height: 100%;
    min-height: 100%;
    margin: 0;
  }

  .v-application,
  .v-application__wrap {
    min-height: 100vh;
    height: 100%;
  }

  .app-top-bar {
    backdrop-filter: blur(10px);
    background: transparent !important;
  }

  .app-top-bar--dark {
    background: rgb(var(--v-theme-surface)) !important;
  }

  .app-brand {
    letter-spacing: -0.01em;
  }

  .app-bar-search-slot {
    flex: 0 1 40vw;
    max-width: 40vw;
  }

  .app-bar-main {
    display: flex;
    align-items: center;
    gap: 1rem;
    min-width: 0;
  }

  .app-bar-title-slot {
    flex: 1 1 auto;
    min-width: 0;
  }

  .app-bar-actions-slot {
    display: flex;
    align-items: center;
    min-width: 0;
  }
  .app-shell-loader {
    position: fixed;
    top: var(--v-layout-top, 64px);
    left: 0;
    right: 0;
    z-index: 1005;
  }

  .app-main {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }

  .app-main-content {
    flex: 1 1 auto;
    min-height: 0;
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  .app-main-content > * {
    flex: 1 1 auto;
    min-height: 0;
    height: 100%;
  }

  .v-navigation-drawer {
    .rail-item {
      .v-list-item__overlay {
        opacity: 0;
      }

      &.v-list-item--active {
        background-color: rgba(255, 255, 255, 0.12);
      }

      &:hover {
        background-color: rgba(255, 255, 255, 0.08);
      }
    }
  }

  @media (max-width: 959px) {
    .app-top-bar {
      padding-inline-end: 0.5rem !important;
    }

    .app-bar-search-slot {
      flex: 1 1 auto;
      max-width: none;
    }

    .app-bar-main {
      margin-left: 0 !important;
      padding-right: 0.25rem;
    }
  }
</style>
