/**
 * Thread composer store for passing message parts between views.
 *
 * When a user starts a new thread from the landing page (e.g. by typing a
 * message or clicking a suggestion chip), the message parts are stored here
 * before navigating to the thread view. The thread view then consumes the
 * pending parts to send the initial message, avoiding data loss during
 * navigation.
 *
 * Flow: LandingView → `setPendingParts()` → Router push → ThreadView → `consumePendingParts()`
 *
 * @module store/threadComposer
 */
import type { MessagePart } from '@/components/ContentInput/types'
import { defineStore } from 'pinia'
import { ref } from 'vue'

/**
 * Pinia store that acts as a transient buffer for message parts
 * between the landing page and a newly created thread view.
 */
export const useThreadComposerStore = defineStore('threadComposer', () => {
  /** Message parts waiting to be sent in a new thread, or `null` if none. */
  const pendingParts = ref<MessagePart[] | null>(null)

  /**
   * Stores message parts to be consumed by the next thread view.
   * Clears the buffer if the provided array is empty.
   *
   * @param parts - The message parts to store (text, file, audio).
   */
  function setPendingParts(parts: MessagePart[]) {
    pendingParts.value = parts.length > 0 ? [...parts] : null
  }

  /**
   * Retrieves and clears the pending message parts (consume-once pattern).
   * Returns `null` if no parts are pending.
   *
   * @returns The stored message parts, or `null` if the buffer is empty.
   */
  function consumePendingParts(): MessagePart[] | null {
    const p = pendingParts.value
    pendingParts.value = null
    return p
  }

  return {
    pendingParts,
    setPendingParts,
    consumePendingParts,
  }
})
