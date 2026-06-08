<!--
  Hosts a single A2UI surface: connects the shared thread MessageProcessor to the Vuetify renderer’s
  A2UIProvider + root ComponentNode. User actions bubble up via inject(a2uiSendActionKey) (provided
  by ThreadView) and are sent to the agent as streaming messages.
-->
<template>
  <div class="a2ui-surface-inline w-100">
    <A2UIProvider
      v-if="processor"
      :processor="processor"
      :surface-id="surfaceId"
      :on-action="onA2UIAction"
    >
      <ComponentNode id="root" />
    </A2UIProvider>
  </div>
</template>

<script setup lang="ts">
  import { a2uiSendActionKey, getOrCreateA2uiProcessor } from '@/a2ui'
  import { A2UIProvider, ComponentNode } from '@alis-build/a2ui-vuetify-renderer'
  import type { A2uiClientAction } from '@a2ui/web_core/v0_9'
  import { computed, inject } from 'vue'

  const props = defineProps<{
    /** Route thread id — used as key for getOrCreateA2uiProcessor. */
    threadId: string
    /** Surface id from the agent’s createSurface envelope. */
    surfaceId: string
  }>()

  /** Parent thread view provides the implementation that POSTs/streams the action to the agent. */
  const sendFromParent = inject(a2uiSendActionKey, undefined)

  /** One MessageProcessor per scope; shared with thread ingestion so server updates apply here. */
  const processor = computed(() => {
    if (!props.threadId) return undefined
    return getOrCreateA2uiProcessor(props.threadId)
  })

  /** Forwards renderer callbacks to the injected sender, defaulting surfaceId when the payload omits it. */
  async function onA2UIAction(action: A2uiClientAction) {
    if (sendFromParent) {
      const enrichedAction: A2uiClientAction = {
        ...action,
        surfaceId: action.surfaceId ?? props.surfaceId,
      }
      await sendFromParent(enrichedAction)
    } else {
      console.warn('[A2UI] No send handler injected (a2uiSendActionKey)')
    }
  }
</script>

<style scoped>
  .a2ui-surface-inline {
    max-width: 100%;
  }
</style>
