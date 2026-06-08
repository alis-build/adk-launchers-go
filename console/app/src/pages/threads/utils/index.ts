/**
 * Utility functions for parsing and building A2A thread resource names.
 *
 * Resource names (see history.proto):
 * - Thread: `threads/{context_id}`
 * - ThreadEvent (canonical): **`threads/{context_id}/events/{event_id}`**
 *
 * @module threads/utils
 */

/**
 * Builds a Thread resource name from a context ID.
 */
export function buildThreadName(contextId: string): string {
  return `threads/${contextId}`
}

/**
 * Builds a ThreadEvent resource name in the canonical form:
 * `threads/{context_id}/events/{event_id}`.
 */
export function buildThreadEventResourceName(contextId: string, eventId: string): string {
  return `threads/${contextId}/events/${eventId}`
}

/**
 * Builds a ThreadEvent name by appending `/events/{eventId}` to a Thread resource name.
 * Equivalent to {@link buildThreadEventResourceName} when `threadName` is exactly `threads/{context_id}`.
 */
export function buildThreadEventName(threadName: string, eventId: string): string {
  return `${threadName}/events/${eventId}`
}

/**
 * Resolves the A2A `context_id` from either:
 *
 * - A **Thread resource name** from APIs: `threads/{context_id}` (always starts with `threads/`).
 * - The **Vue Router** URL is `/threads/{context_id}`: the named param `threadId` is the bare
 *   `{context_id}` only. The `threads/` prefix is the static path segment, not part of the param.
 *   Navigate with `` `/threads/${contextId}` `` (no encoding). API resource names remain
 *   `threads/{context_id}` from {@link buildThreadName}.
 */
export function getContextIdFromThreadName(name: string): string {
  if (!name || typeof name !== 'string') {
    return ''
  }
  const match = name.match(/^threads\/([^/]+)/)
  if (match?.[1]) {
    return match[1]
  }
  // Bare context id (e.g. route segment after `/threads/`)
  if (!name.includes('/')) {
    return name
  }
  return ''
}

/**
 * Extracts the event id from a ThreadEvent name (`threads/{context_id}/events/{event_id}`).
 */
export function getEventIdFromEventName(name: string): string {
  if (!name || typeof name !== 'string') {
    return ''
  }
  const match = name.match(/^threads\/[^/]+\/events\/([^/]+)$/)
  return match?.[1] ?? ''
}
