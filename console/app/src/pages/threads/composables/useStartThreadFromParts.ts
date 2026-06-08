/**
 * Composable for starting a new thread from pre-composed message parts.
 *
 * Used by the landing page and suggestion chips to navigate to a new
 * thread with an initial message already prepared. The flow is:
 * 1. Generate a new UUIDv7 context ID (doubles as the thread/session ID).
 * 2. Store the message parts in the `threadComposerStore` transient buffer.
 * 3. Push the router to `/threads/{contextId}?is_new_thread=true`.
 * 4. The `ThreadView` consumes the pending parts on mount and sends them.
 *
 * @module pages/threads/composables/useStartThreadFromParts
 */
import type { MessagePart } from '@/components/ContentInput/types'
import { useThreadComposerStore } from '@/store/threadComposer'
import { v7 as uuidv7 } from 'uuid'
import { useRouter } from 'vue-router'

/**
 * Returns a `startFromParts` function that navigates to a new thread
 * with the given message parts queued for immediate sending.
 *
 * @returns An object with a single `startFromParts` method.
 *
 * @example
 * ```ts
 * const { startFromParts } = useStartThreadFromParts()
 * startFromParts([new TextMessagePart('Hello!')])
 * // → Navigates to /threads/{new-uuid}?is_new_thread=true
 * ```
 */
export function useStartThreadFromParts() {
  const router = useRouter()
  const threadComposerStore = useThreadComposerStore()

  /**
   * Creates a new thread and navigates to it with the provided message parts.
   *
   * @param parts - The message parts (text, file, audio) to send as the first message.
   */
  function startFromParts(parts: MessagePart[]) {
    const contextId = uuidv7()
    threadComposerStore.setPendingParts(parts)
    void router.push({
      path: `/threads/${contextId}`,
      query: { is_new_thread: 'true' },
    })
  }

  return { startFromParts }
}
