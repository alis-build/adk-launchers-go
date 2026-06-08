/**
 * A2UI message detection and classification helpers.
 *
 * These predicates inspect normalized data-part objects to determine whether
 * they are A2UI v0.9 envelopes, and if so, whether they should render inline
 * in the chat (only `createSurface`) or be silently consumed by the processor
 * (incremental updates like `updateComponents` / `updateDataModel`).
 *
 * @module a2ui/detection
 */

/** The four A2UI v0.9 envelope operation keys. */
const A2UI_ENVELOPE_KEYS = ['createSurface', 'updateComponents', 'updateDataModel', 'deleteSurface'] as const

/**
 * Unwrap the optional `{ a2ui: { ... } }` wrapper that some agents send.
 */
function unwrapA2UITarget(obj: Record<string, unknown>): Record<string, unknown> {
  return typeof obj.a2ui === 'object' && obj.a2ui !== null
    ? (obj.a2ui as Record<string, unknown>)
    : obj
}

/**
 * True if the normalized data part looks like an A2UI v0.9 server-to-client
 * envelope (possibly without `version` field, possibly wrapped in `{ a2ui: ... }`).
 *
 * Also handles arrays — returns `true` if any element is an A2UI message.
 */
export function isA2UIMessage(obj: unknown): boolean {
  if (!obj || typeof obj !== 'object') return false
  if (Array.isArray(obj)) {
    return obj.some((item) => isA2UIMessage(item))
  }
  const target = unwrapA2UITarget(obj as Record<string, unknown>)
  return A2UI_ENVELOPE_KEYS.some((k) => target[k] !== undefined && target[k] !== null)
}

/**
 * Whether this data part should render visible content in the chat bubble.
 *
 * Only `createSurface` envelopes are rendered inline (they produce an
 * {@link A2UISurface} component). Incremental updates (`updateComponents`,
 * `updateDataModel`, `deleteSurface`) are processed silently by the
 * {@link MessageProcessor} — showing them as bubbles would be noisy.
 *
 * @param obj - A normalized data-part object.
 * @returns `true` if the part should render in chat, `false` to hide it.
 */
export function shouldRenderA2UIDataPartInChat(obj: unknown): boolean {
  if (!isA2UIMessage(obj)) return true
  if (Array.isArray(obj)) {
    return obj.some((item) => {
      const target = unwrapA2UITarget(item as Record<string, unknown>)
      return target.createSurface !== undefined && target.createSurface !== null
    })
  }
  const target = unwrapA2UITarget(obj as Record<string, unknown>)
  return target.createSurface !== undefined && target.createSurface !== null
}

/**
 * Extract the `surfaceId` from a `createSurface` envelope.
 *
 * Used by {@link DataPart} to decide whether to render an {@link A2UISurface}
 * component instead of a raw JSON expansion panel.
 *
 * @param obj - A normalized data-part object.
 * @returns The surface ID string, or `undefined` if not a `createSurface` envelope.
 */
export function getA2UICreateSurfaceId(obj: unknown): string | undefined {
  if (!obj || typeof obj !== 'object') return undefined
  if (Array.isArray(obj)) {
    for (const item of obj) {
      const id = getA2UICreateSurfaceId(item)
      if (id) return id
    }
    return undefined
  }
  const target = unwrapA2UITarget(obj as Record<string, unknown>)
  const cs = target.createSurface
  if (cs && typeof cs === 'object' && cs !== null) {
    const id = (cs as { surfaceId?: unknown }).surfaceId
    return typeof id === 'string' && id.length > 0 ? id : undefined
  }
  return undefined
}
