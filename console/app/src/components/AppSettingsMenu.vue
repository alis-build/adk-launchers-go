<script setup lang="ts">
  import { useA2UISettingsStore } from '@/a2ui'
  import { computed, watch } from 'vue'
  import { useTheme } from 'vuetify'

  defineProps<{
    /** Whether the navigation rail is expanded (affects menu layout). */
    expanded: boolean
  }>()

  const a2uiSettingsStore = useA2UISettingsStore()

  const a2uiHelperText = computed(() =>
    a2uiSettingsStore.a2uiToggleDisabled
      ? 'Not available for this agent yet.'
      : 'Enable rich interactive UI surfaces from agents.',
  )
  const theme = useTheme()
  const isLightTheme = computed(() => !theme.global.current.value.dark)

  const isDarkMode = computed({
    get: () => theme.global.current.value.dark,
    set: (value: boolean) => {
      theme.change(value ? 'darkTheme' : 'lightTheme')
    },
  })

  watch(
    isDarkMode,
    (value) => {
      localStorage.setItem('app-theme', value ? 'darkTheme' : 'lightTheme')
    },
    { immediate: true },
  )
</script>

<template>
  <v-menu
    location="top end"
    offset="12"
  >
    <template #activator="{ props: menuProps, isActive }">
      <v-btn
        v-if="expanded"
        block
        rounded="pill"
        variant="text"
        class="justify-start text-none settings-trigger"
        :class="{ 'settings-trigger--active': isActive, 'settings-trigger--light': isActive && isLightTheme }"
        v-bind="menuProps"
      >
        <v-icon class="mr-3">settings</v-icon>
        Settings and help
      </v-btn>
      <v-btn
        v-else
        icon
        variant="text"
        class="settings-trigger mx-auto"
        :class="{ 'settings-trigger--active': isActive, 'settings-trigger--light': isActive && isLightTheme }"
        aria-label="Settings and help"
        v-bind="menuProps"
      >
        <v-icon>settings</v-icon>
        <v-tooltip activator="parent">Settings and help</v-tooltip>
      </v-btn>
    </template>

    <v-card
      min-width="320"
      rounded="xl"
      class="settings-menu"
    >
      <v-list
        bg-color="transparent"
        class="py-2"
      >
        <div class="px-4 py-2 d-flex align-center justify-space-between ga-4">
          <div>
            <div class="text-title-medium">Dark mode</div>
            <div class="text-body-small text-medium-emphasis">
              Switch between light and dark appearance.
            </div>
          </div>
          <v-switch
            v-model="isDarkMode"
            color="primary"
            hide-details
            inset
          />
        </div>

        <v-divider class="mx-4" />

        <div
          class="px-4 py-2 d-flex align-center justify-space-between ga-4"
          :class="{ 'opacity-60': a2uiSettingsStore.a2uiToggleDisabled }"
        >
          <div>
            <div class="text-title-medium">A2UI</div>
            <div class="text-body-small text-medium-emphasis">
              {{ a2uiHelperText }}
            </div>
          </div>
          <v-switch
            v-model="a2uiSettingsStore.a2uiEnabled"
            color="primary"
            hide-details
            inset
            :disabled="a2uiSettingsStore.a2uiToggleDisabled"
            :readonly="a2uiSettingsStore.a2uiToggleDisabled"
          />
        </div>
      </v-list>
    </v-card>
  </v-menu>
</template>

<style scoped lang="scss">
  .settings-trigger {
    color: rgb(var(--v-theme-sidebar-text));
    background: rgba(var(--v-theme-sidebar-text), 0.05);
  }

  .settings-trigger--active {
    background: rgb(var(--v-theme-sidebar-active));
  }

  .settings-menu {
    color: rgb(var(--v-theme-sidebar-text));
    background: rgb(var(--v-theme-sidebar)) !important;
    border: 1px solid rgb(var(--v-theme-sidebar-border));
  }

  .settings-trigger--light {
    color: #3358a8;
  }
</style>
