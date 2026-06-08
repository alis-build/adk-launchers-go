<script setup lang="ts">
  import type { MessagePart } from '@/components/ContentInput/types'
  import { TextMessagePart } from '@/components/ContentInput/types'
  import ThreadComposerCard from '@/pages/threads/components/ThreadComposerCard.vue'
  import { useStartThreadFromParts } from '@/pages/threads/composables/useStartThreadFromParts'
  import { SUGGESTION_CHIPS } from '@/pages/threads/config/suggestionChips'
  import { useAppStore } from '@/store/app'
  import { computed } from 'vue'
  import { useTheme } from 'vuetify'

  const appStore = useAppStore()
  const theme = useTheme()
  const { startFromParts } = useStartThreadFromParts()
  const firstName = computed(() => appStore.userDisplayName.split(' ')[0] || 'there')
  const isDarkTheme = computed(() => theme.global.current.value.dark)

  function onSubmit(parts: MessagePart[]) {
    startFromParts(parts)
  }

  function onChipClick(text: string) {
    startFromParts([new TextMessagePart(text)])
  }
</script>

<template>
  <v-container class="threads-landing-page fill-height py-6 py-md-10">
    <v-row
      justify="center"
      align="center"
      class="fill-height"
    >
      <v-col
        cols="12"
        md="11"
        lg="9"
        xl="8"
      >
        <div class="landing-inner">
          <div class="hero-block">
            <div class="hero-greeting text-title-large">
              <v-icon
                size="20"
                color="primary"
                class="mr-2"
              >
                auto_awesome
              </v-icon>
              Hi {{ firstName }}
            </div>
            <div class="hero-title text-display-small">Where should we start?</div>
          </div>

          <ThreadComposerCard
            placeholder="Enter a prompt for your agent"
            @submit="onSubmit"
          />

          <div
            class="chips-row"
            :class="{ 'chips-row--dark': isDarkTheme }"
          >
            <v-chip
              v-for="chip in SUGGESTION_CHIPS"
              :key="chip.id"
              class="suggestion-chip"
              color="surfaceContainer"
              density="comfortable"
              rounded="pill"
              variant="flat"
              @click="onChipClick(chip.text)"
            >
              {{ chip.label }}
            </v-chip>
          </div>
        </div>
      </v-col>
    </v-row>
  </v-container>
</template>

<style lang="scss" scoped>
  .threads-landing-page {
    align-items: stretch;
  }

  .landing-inner {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    justify-content: center;
    min-height: min(720px, 100%);
  }

  .hero-block {
    margin-bottom: 1.5rem;
    text-align: left;
  }

  .hero-greeting {
    display: flex;
    align-items: center;
    color: rgba(var(--v-theme-on-surface), 0.92);
    margin-bottom: 0.5rem;
  }

  .hero-title {
    letter-spacing: -0.03em;
    line-height: 1.05;
    max-width: 620px;
  }

  .chips-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    margin-top: 1.25rem;
  }

  .suggestion-chip {
    margin: 0;
    background: rgb(var(--v-theme-surfaceContainer)) !important;
    color: rgba(var(--v-theme-on-surface), 0.84) !important;
  }

  .chips-row--dark .suggestion-chip {
    background: rgb(var(--v-theme-surfaceContainerHigh)) !important;
  }

  @media (max-width: 960px) {
    .landing-inner {
      min-height: auto;
    }
  }
</style>
