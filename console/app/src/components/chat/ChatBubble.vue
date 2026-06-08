<script setup lang="ts">
  import { getA2UICreateSurfaceId } from '@/a2ui'
  import type { ChatMessage } from '@/pages/threads/types'
  import { applyToolResultToCall } from '@/pages/threads/store/toolCallStatus'
  import { useSnackbarStore } from '@/store/snackbar'
  import { computed } from 'vue'
  import { useRoute } from 'vue-router'
  import A2UISurface from './A2UISurface.vue'
  import ChatFilePart from './ChatFilePart.vue'
  import ChatTextContent from './ChatTextContent.vue'
  import ChatToolCallPanel from './ChatToolCallPanel.vue'

  interface Props {
    /** Message to render (text, tools, files, A2UI payloads). */
    message: ChatMessage
    /** When true, show details action that emits `click`. */
    clickable?: boolean
    /** Optional link for trace/debug navigation. */
    traceHref?: string
    /** Tool call IDs hidden while confirmation UI is active. */
    hiddenToolCallIds?: string[]
  }

  const props = withDefaults(defineProps<Props>(), {
    clickable: false,
    traceHref: undefined,
    hiddenToolCallIds: () => [],
  })

  const snackbarStore = useSnackbarStore()
  const route = useRoute()

  const emit = defineEmits<{
    (e: 'click', message: ChatMessage): void
    (e: 'debug', message: ChatMessage): void
  }>()

  const isUser = computed(() => props.message.role === 'user')
  const threadId = computed(() => (route.params.threadId as string) ?? '')

  /** Merges streamed tool results into matching toolCalls for display. */
  const enrichedToolCalls = computed(() => {
    const calls = props.message.toolCalls ?? []
    const results = props.message.toolResults ?? []
    const merged = !results.length
      ? calls
      : calls.map((tc) => {
          const match = results.find((r) => r.toolCallId === tc.id)
          if (!match) return tc
          return applyToolResultToCall(tc, match.content)
        })
    const hidden = new Set(props.hiddenToolCallIds ?? [])
    return merged.filter((tc) => !hidden.has(tc.id))
  })

  /** True when some tool calls are hidden because their confirmation UI is active. */
  const hasHiddenToolCalls = computed(() => {
    const hidden = new Set(props.hiddenToolCallIds ?? [])
    return (props.message.toolCalls ?? []).some((tc) => hidden.has(tc.id))
  })

  const a2uiSurfaces = computed(() => {
    if (!props.message.a2uiPayloads?.length) return []
    return props.message.a2uiPayloads
      .map((p) => {
        const surfaceId = getA2UICreateSurfaceId(p as Record<string, unknown>)
        return surfaceId ? { surfaceId } : null
      })
      .filter(Boolean) as { surfaceId: string }[]
  })

  const hasContent = computed(() =>
    !!props.message.content ||
    (props.message.files?.length ?? 0) > 0 ||
    enrichedToolCalls.value.length > 0 ||
    hasHiddenToolCalls.value ||
    a2uiSurfaces.value.length > 0,
  )

  const handleOpenDetails = () => {
    if (props.clickable) {
      emit('click', props.message)
    }
  }

  const handleOpenDebug = () => {
    if (props.clickable) {
      emit('debug', props.message)
    }
  }

  const handleCopy = async () => {
    const segments: string[] = []
    if (props.message.content) segments.push(props.message.content)
    for (const tc of enrichedToolCalls.value) {
      segments.push(JSON.stringify(tc.args, null, 2))
    }
    const toCopy = segments.join('\n\n').trim()
    if (!toCopy) {
      snackbarStore.error('No text to copy')
      return
    }
    try {
      await navigator.clipboard.writeText(toCopy)
      snackbarStore.success('Copied to clipboard')
    } catch (error) {
      console.error('Failed to copy:', error)
      snackbarStore.error('Failed to copy to clipboard')
    }
  }
</script>

<template>
  <v-hover v-if="hasContent">
    <template #default="{ isHovering, props: hoverProps }">
      <div
        v-bind="hoverProps"
        :class="['message-bubble-wrapper', { 'message-bubble-wrapper--user': isUser, 'message-bubble-wrapper--agent': !isUser }]"
      >
        <div :class="['message-bubble px-4', { 'message-bubble--user rounded-s-xl rounded-be-xl rounded-te py-3': isUser, 'message-agent': !isUser }]">
          <ChatTextContent
            v-if="message.content"
            :content="message.content"
            class="message-part"
          />

          <ChatFilePart
            v-for="(file, idx) in (message.files ?? [])"
            :key="`file-${idx}`"
            :file="file"
            class="message-part"
          />

          <v-alert
            v-if="hasHiddenToolCalls"
            type="info"
            variant="tonal"
            density="compact"
            class="message-part"
          >
            Tool confirmation required — see the form below.
          </v-alert>

          <ChatToolCallPanel
            v-for="(tc, idx) in enrichedToolCalls"
            :key="`tc-${tc.id || idx}`"
            :tool-call="tc"
            class="message-part"
          />

          <A2UISurface
            v-for="(surface, idx) in a2uiSurfaces"
            :key="`a2ui-${idx}`"
            :thread-id="threadId"
            :surface-id="surface.surfaceId"
            class="message-part"
          />
        </div>
        <div
          v-if="clickable"
          :class="['actions-wrapper', { 'actions-wrapper--visible': isHovering, 'actions-wrapper--user': isUser, 'actions-wrapper--agent': !isUser }]"
        >
          <v-btn
            size="small"
            icon
            variant="plain"
            @click.stop="handleCopy"
          >
            <v-icon size="16">content_copy</v-icon>
          </v-btn>
          <v-btn
            size="small"
            icon
            variant="plain"
            @click.stop="handleOpenDetails"
          >
            <v-icon size="16">info</v-icon>
          </v-btn>

          <v-btn
            size="small"
            icon
            variant="plain"
            @click.stop="handleOpenDebug"
          >
            <v-icon size="16">bug_report</v-icon>
          </v-btn>

          <v-btn
            v-if="traceHref"
            size="small"
            icon
            variant="plain"
            :href="traceHref"
            target="_blank"
            rel="noopener noreferrer"
          >
            <v-icon size="16">troubleshoot</v-icon>
          </v-btn>
        </div>
      </div>
    </template>
  </v-hover>
</template>

<style lang="scss" scoped>
  .message-part {
    margin-top: 4px;

    &:first-child {
      margin-top: 0;
    }
  }

  .message-bubble-wrapper {
    display: inline-flex;
    flex-direction: column;
    width: 100%;

    &--user {
      align-items: flex-end;
    }

    &--agent {
      align-items: flex-start;
    }
  }

  .message-bubble {
    width: 100%;
    max-width: 100%;
  }

  .message-bubble--user {
    background: rgb(var(--v-theme-secondarySurfaceContainer));
    color: rgb(var(--v-theme-onBackground));
    box-shadow: inset 0 1px 0 rgba(var(--v-theme-onPrimary), 0.06);
  }

  .message-bubble-wrapper--user .message-bubble {
    width: auto;
    max-width: min(42rem, 78%);
  }

  .actions-wrapper {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 2px;
    margin-top: 4px;
    height: 24px;
    opacity: 0;
    transition: opacity 0.15s ease;

    &--visible {
      opacity: 1;
    }

    &--user {
      justify-content: flex-end;
    }

    &--agent {
      justify-content: flex-start;
    }
  }
</style>
