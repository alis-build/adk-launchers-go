<script setup lang="ts">
  import type { ChatToolCall } from '@/pages/threads/types'
  import { computed, shallowRef } from 'vue'

  const props = defineProps<{
    /** Tool invocation to display (name, args, optional result). */
    toolCall: ChatToolCall
  }>()

  const expanded = shallowRef(false)

  /** Derives display status: explicit status → result payload inspection → default pending. */
  const functionStatus = computed((): 'completed' | 'error' | 'pending' => {
    if (props.toolCall.status === 'error') return 'error'
    if (props.toolCall.status === 'pending') return 'pending'
    if (props.toolCall.status === 'completed') return 'completed'
    if (props.toolCall.result !== undefined) {
      if (typeof props.toolCall.result === 'object' && props.toolCall.result !== null) {
        const obj = props.toolCall.result as Record<string, unknown>
        if (typeof obj.error === 'string' && obj.error) return 'error'
        if (obj.status === 'pending_confirmation') return 'pending'
      }
      return 'completed'
    }
    return 'pending'
  })

  const formattedArgs = computed(() => {
    try {
      return JSON.stringify(props.toolCall.args ?? {}, null, 2)
    } catch {
      return '{}'
    }
  })

  const formattedResult = computed(() => {
    try {
      return JSON.stringify(props.toolCall.result, null, 2)
    } catch {
      return 'Invalid data'
    }
  })
</script>

<template>
  <v-card
    variant="outlined"
    rounded="lg"
    class="function-call-card"
  >
    <div
      class="function-call-header"
      @click="expanded = !expanded"
    >
      <v-icon
        size="18"
        class="function-call-icon"
      >
        build
      </v-icon>
      <span class="function-call-name">{{ toolCall.name }}</span>
      <v-chip
        size="x-small"
        variant="tonal"
        :color="functionStatus === 'error' ? 'error' : functionStatus === 'pending' ? 'warning' : 'success'"
        class="function-call-status"
      >
        <v-icon
          start
          size="10"
        >
          {{ functionStatus === 'error' ? 'error' : functionStatus === 'pending' ? 'schedule' : 'check_circle' }}
        </v-icon>
        {{ functionStatus === 'error' ? 'Error' : functionStatus === 'pending' ? 'Awaiting confirmation' : 'Completed' }}
      </v-chip>
      <v-icon
        size="18"
        class="function-call-chevron"
        :class="{ 'function-call-chevron--open': expanded }"
      >
        expand_more
      </v-icon>
    </div>

    <v-expand-transition>
      <div
        v-if="expanded"
        class="function-call-body"
      >
        <div class="function-call-section">
          <div class="function-call-section-label">Arguments</div>
          <pre class="function-call-code">{{ formattedArgs }}</pre>
        </div>
        <div
          v-if="toolCall.result !== undefined"
          class="function-call-section"
        >
          <div class="function-call-section-label">Result</div>
          <pre class="function-call-code">{{ formattedResult }}</pre>
        </div>
      </div>
    </v-expand-transition>
  </v-card>
</template>

<style lang="scss" scoped>
  .function-call-card {
    margin-top: 4px;
    overflow: hidden;
  }

  .function-call-header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    cursor: pointer;
    user-select: none;
    transition: background-color 0.12s ease;

    &:hover {
      background-color: rgba(var(--v-theme-on-surface), 0.04);
    }
  }

  .function-call-icon {
    color: rgba(var(--v-theme-on-surface), 0.6);
    flex-shrink: 0;
  }

  .function-call-name {
    font-family: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 12.5px;
    font-weight: 500;
    color: rgb(var(--v-theme-on-surface));
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .function-call-status {
    flex-shrink: 0;
  }

  .function-call-chevron {
    color: rgba(var(--v-theme-on-surface), 0.5);
    flex-shrink: 0;
    transition: transform 0.2s ease;

    &--open {
      transform: rotate(180deg);
    }
  }

  .function-call-body {
    border-top: 1px solid rgba(var(--v-theme-on-surface), 0.08);
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    background-color: rgba(var(--v-theme-on-surface), 0.02);
  }

  .function-call-section {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .function-call-section-label {
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: 600;
    color: rgba(var(--v-theme-on-surface), 0.5);
  }

  .function-call-code {
    font-family: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 11.5px;
    line-height: 1.55;
    color: rgba(var(--v-theme-on-surface), 0.8);
    background-color: rgba(var(--v-theme-on-surface), 0.04);
    padding: 10px 12px;
    border-radius: 8px;
    white-space: pre-wrap;
    word-break: break-word;
    overflow-x: auto;
    margin: 0;
  }
</style>
