/**
 * Dialog state store for the thread view.
 *
 * Manages the open/close state and content for two modal dialogs:
 * - **Details dialog** - Shows message details and artifact envelope metadata.
 * - **Debug dialog** - Shows raw AG-UI message / SSE event JSON for developer inspection.
 *
 * @module pages/threads/store/threadViewDialog
 */
import type { ChatMessage } from '@/pages/threads/types'
import { defineStore } from 'pinia'
import { ref, type Ref } from 'vue'

const STORE_ID = 'threadViewDialog'

/**
 * Pinia store managing dialog visibility and the selected message for
 * the details and debug inspection dialogs in the thread view.
 */
export const useThreadViewDialogStore = defineStore(STORE_ID, () => {
  /** Whether the message details dialog is currently open. */
  const detailsDialogOpen: Ref<boolean> = ref(false)
  /** The ChatMessage currently displayed in the details dialog. */
  const detailsDialogMessage: Ref<ChatMessage | null> = ref(null)
  /** Whether the debug (raw JSON) dialog is currently open. */
  const debugDialogOpen: Ref<boolean> = ref(false)
  /** The ChatMessage currently displayed in the debug dialog. */
  const debugDialogMessage: Ref<ChatMessage | null> = ref(null)
  /** Optional artifact envelope metadata shown in the details dialog. */
  const detailsDialogArtifactEnvelopeMetadata: Ref<Record<string, unknown> | undefined> = ref(undefined)

  /**
   * Opens the details dialog for a specific message.
   *
   * @param message - The ChatMessage to display.
   * @param artifactEnvelopeMetadata - Optional metadata from the artifact update envelope.
   */
  function openDetailsDialogForMessage(
    message: ChatMessage,
    artifactEnvelopeMetadata?: Record<string, unknown> | undefined,
  ) {
    detailsDialogMessage.value = message
    detailsDialogArtifactEnvelopeMetadata.value = artifactEnvelopeMetadata
    detailsDialogOpen.value = true
  }

  /** Closes both dialogs and clears their associated message data. */
  function resetDialogState() {
    detailsDialogOpen.value = false
    detailsDialogMessage.value = null
    debugDialogOpen.value = false
    debugDialogMessage.value = null
    detailsDialogArtifactEnvelopeMetadata.value = undefined
  }

  /**
   * Opens the debug dialog to display raw AG-UI debug JSON for a message.
   * @param message - The ChatMessage whose debug data to display.
   */
  function openDebugDialogForMessage(message: ChatMessage) {
    debugDialogMessage.value = message
    debugDialogOpen.value = true
  }

  /** Closes the debug dialog and clears its message data. */
  function closeDebugDialog() {
    debugDialogOpen.value = false
    debugDialogMessage.value = null
  }

  return {
    detailsDialogOpen,
    detailsDialogMessage,
    debugDialogOpen,
    debugDialogMessage,
    detailsDialogArtifactEnvelopeMetadata,
    openDetailsDialogForMessage,
    openDebugDialogForMessage,
    closeDebugDialog,
    resetDialogState,
  }
})
