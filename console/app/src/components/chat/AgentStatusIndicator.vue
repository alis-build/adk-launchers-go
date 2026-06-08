<script setup lang="ts">
  import type { ChatStatus } from '@/pages/threads/types'
  import { computed, ref } from 'vue'
  import AgentWorkingIndicator from '@/components/chat/AgentWorkingIndicator.vue'
  import ChatTextContent from '@/components/chat/ChatTextContent.vue'

  interface Props {
    /** Latest task status for this agent turn. */
    status: ChatStatus
    /** Override default awaiting_confirmation hint. */
    confirmationHint?: string
    /** Completed reasoning snippets from history. */
    thoughts?: string[]
    /** In-flight reasoning text during streaming. */
    liveReasoning?: string
    /** Error text when status is `failed`. */
    errorMessage?: string
    /** When true, emit `click` for details navigation. */
    clickable?: boolean
  }

  const props = withDefaults(defineProps<Props>(), {
    clickable: false,
    thoughts: () => [],
    liveReasoning: '',
    errorMessage: undefined,
  })

  const emit = defineEmits<{
    /** User opened status/details for this turn. */
    (e: 'click'): void
  }>()

  const statusIndicatorVariant = computed<'star' | 'star-orbit-comet-5' | 'star-orbit-comet-5-reverse' | 'star-error'>(() => {
    if (props.status === 'completed') return 'star'
    if (props.status === 'failed') return 'star-error'
    if (props.status === 'awaiting_confirmation') return 'star-orbit-comet-5-reverse'
    return 'star-orbit-comet-5'
  })

  /** Inline hint shown next to the indicator when awaiting user confirmation. */
  const statusHint = computed(() => {
    if (props.status === 'awaiting_confirmation') {
      return props.confirmationHint ?? 'Needs your OK to continue'
    }
    return undefined
  })

  const hasThoughts = computed(() => props.thoughts.length > 0)
  const hasLiveReasoning = computed(() => !!props.liveReasoning)
  const showThinkingToggle = computed(() => hasThoughts.value || hasLiveReasoning.value)
  const expanded = ref(false)
  const errorExpanded = ref(true)

  const handleClick = () => {
    if (props.clickable) {
      emit('click')
    }
  }
</script>

<template>
  <div class="agent-status-indicator mt-2">
    <div
      class="d-flex align-center pl-1"
      :class="{ 'agent-status-clickable': clickable }"
      @click="handleClick"
    >
      <AgentWorkingIndicator :variant="statusIndicatorVariant" />

      <span
        v-if="statusHint"
        class="text-body-small text-medium-emphasis ml-2"
      >
        {{ statusHint }}
      </span>

      <div
        v-if="showThinkingToggle"
        class="thinking-toggle"
      >
        <v-btn
          rounded="pill"
          variant="text"
          class="thinking-button text-none"
          @click.stop="expanded = !expanded"
        >
          <span class="mr-2 text-body-small">{{ expanded ? 'Hide thinking' : 'Show thinking' }}</span>
          <v-icon size="18">{{ expanded ? 'keyboard_arrow_up' : 'keyboard_arrow_down' }}</v-icon>
        </v-btn>
      </div>

      <div
        v-if="errorMessage"
        class="thinking-toggle ml-2"
      >
        <v-btn
          rounded="pill"
          variant="text"
          class="error-button text-none text-error"
          @click.stop="errorExpanded = !errorExpanded"
        >
          <span class="mr-2 text-body-small">{{ errorExpanded ? 'Hide error' : 'Show error' }}</span>
          <v-icon size="18">{{ errorExpanded ? 'keyboard_arrow_up' : 'keyboard_arrow_down' }}</v-icon>
        </v-btn>
      </div>
    </div>

    <v-expand-transition>
      <div
        v-if="expanded && (hasThoughts || hasLiveReasoning)"
        class="px-4 pt-0"
      >
        <div
          v-for="(line, idx) in thoughts"
          :key="idx"
          class="text-medium-emphasis ml-7 pl-7 pt-1 border-s"
        >
          <ChatTextContent :content="line" />
        </div>
        <div
          v-if="hasLiveReasoning"
          class="text-medium-emphasis ml-7 pl-7 pt-1 border-s live-reasoning"
        >
          {{ liveReasoning }}
        </div>
      </div>
    </v-expand-transition>

    <v-expand-transition>
      <div
        v-if="errorExpanded && errorMessage"
        class="px-4 pt-0"
      >
        <div class="text-error ml-7 pl-7 pt-1 border-s border-error">
          <ChatTextContent :content="errorMessage" />
        </div>
      </div>
    </v-expand-transition>
  </div>
</template>

<style lang="scss" scoped>
  .agent-status-indicator {
    width: 100%;
  }

  .thinking-toggle {
    width: 100%;
  }

  .thinking-button {
    border-color: rgba(var(--v-theme-primary), 0.7);
    color: rgb(var(--v-theme-on-surface));
  }

  .error-button {
    border-color: rgba(var(--v-theme-error), 0.7);
  }

  .live-reasoning {
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 200px;
    overflow-y: auto;
    padding-bottom: 0.5rem;
  }
</style>
