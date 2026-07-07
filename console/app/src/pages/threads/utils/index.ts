/**
 * Utility functions for parsing and building AG-UI thread resource names.
 *
 * Resource names (see history.proto):
 * - Thread: `threads/{thread_id}`
 * - ThreadEvent (canonical): **`threads/{thread_id}/events/{event_id}`**
 *
 * @module threads/utils
 */

/**
 * Builds a Thread resource name from a thread id.
 */
export function buildThreadName(threadId: string): string {
  return `threads/${threadId}`
}

/**
 * Builds a ThreadEvent resource name in the canonical form:
 * `threads/{thread_id}/events/{event_id}`.
 */
export function buildThreadEventResourceName(threadId: string, eventId: string): string {
  return `threads/${threadId}/events/${eventId}`
}

/**
 * Builds a ThreadEvent name by appending `/events/{eventId}` to a Thread resource name.
 * Equivalent to {@link buildThreadEventResourceName} when `threadName` is exactly `threads/{thread_id}`.
 */
export function buildThreadEventName(threadName: string, eventId: string): string {
  return `${threadName}/events/${eventId}`
}

/**
 * Resolves the thread id from either:
 *
 * - A **Thread resource name** from APIs: `threads/{thread_id}` (always starts with `threads/`).
 * - The **Vue Router** URL `/threads/{thread_id}`: the named param `threadId` is the bare
 *   `{thread_id}` only. The `threads/` prefix is the static path segment, not part of the param.
 *   Navigate with `` `/threads/${threadId}` `` (no encoding). API resource names remain
 *   `threads/{thread_id}` from {@link buildThreadName}.
 *
 * Kept named `getContextIdFromThreadName` for API stability with earlier consumers;
 * the returned value is the AG-UI thread id segment.
 */
export function getContextIdFromThreadName(name: string): string {
  if (!name || typeof name !== 'string') {
    return ''
  }
  const match = name.match(/^threads\/([^/]+)/)
  if (match?.[1]) {
    return match[1]
  }
  // Bare thread id (e.g. route segment after `/threads/`)
  if (!name.includes('/')) {
    return name
  }
  return ''
}

/**
 * Extracts the event id from a ThreadEvent name (`threads/{thread_id}/events/{event_id}`).
 */
export function getEventIdFromEventName(name: string): string {
  if (!name || typeof name !== 'string') {
    return ''
  }
  const match = name.match(/^threads\/[^/]+\/events\/([^/]+)$/)
  return match?.[1] ?? ''
}
