/**
 * A2UI MessageProcessor lifecycle management.
 *
 * Each scope gets its own {@link MessageProcessor} instance that accumulates
 * A2UI surface state (createSurface → updateComponents → updateDataModel → deleteSurface).
 * The processor is consumed by {@link A2UISurface} via `getOrCreateA2uiProcessor`.
 * User gestures call `SurfaceModel.dispatchAction`, which emits on {@link MessageProcessor}'s
 * `model.onAction`. The host view registers a handler via {@link setA2uiActionHandler}
 * so those actions reach the agent (the renderer’s `on-action` prop is not used when
 * `dispatchAction` exists on the surface).
 *
 * A deduplication set per scope ensures each payload key is only processed once.
 *
 * @module a2ui/processor
 */
import { Catalog, MessageProcessor, type A2uiClientAction, type A2uiMessage, type ComponentApi } from '@a2ui/web_core/v0_9'
import { CATALOG_ID, VUETIFY_COMPONENTS, VUETIFY_FUNCTIONS, VUETIFY_THEME_SCHEMA } from '@alis-build/a2ui-vuetify-renderer'
import { isA2UIMessage } from './detection'
import { registerCustomComponents } from './registerCustomComponents'

// Register custom components immediately so they are available when loading from history
registerCustomComponents()

/** Vuetify catalog used by the message processor to resolve component schemas. */
const vuetifyCatalog = new Catalog(CATALOG_ID, VUETIFY_COMPONENTS, VUETIFY_FUNCTIONS, VUETIFY_THEME_SCHEMA)

/** Processor instances keyed by scope key. */
const processors = new Map<string, MessageProcessor<ComponentApi>>()

/** Deduplication: tracks which data-part envelopes have already been fed to each processor. */
const processedEnvelopeKeys = new Map<string, Set<string>>()

/** Forwards `model.onAction` from each scope processor to the active host view (streaming send). */
const scopeActionHandlers = new Map<string, (action: A2uiClientAction) => void | Promise<void>>()

function forwardScopeAction(scopeKey: string, action: A2uiClientAction): void {
  const handler = scopeActionHandlers.get(scopeKey)
  if (!handler) return
  void Promise.resolve(handler(action)).catch((e) => {
    console.error('[A2UI] action handler failed', e)
  })
}

/**
 * Register the callback that receives A2UI client actions for a scope (from `sendAction` /
 * `dispatchNodeAction` → `surface.dispatchAction` → `model.onAction`).
 *
 * Call from the host view when active; unregister the returned function on leave
 * or when switching scopes.
 */
export function setA2uiActionHandler(
  scopeKey: string,
  handler: (action: A2uiClientAction) => void | Promise<void>,
): () => void {
  scopeActionHandlers.set(scopeKey, handler)
  return () => {
    if (scopeActionHandlers.get(scopeKey) === handler) {
      scopeActionHandlers.delete(scopeKey)
    }
  }
}

function getProcessedSet(scopeKey: string): Set<string> {
  let s = processedEnvelopeKeys.get(scopeKey)
  if (!s) {
    s = new Set()
    processedEnvelopeKeys.set(scopeKey, s)
  }
  return s
}

/**
 * Unwrap the optional `{ a2ui: { ... } }` wrapper that some agents put around envelopes.
 */
function unwrapA2UIPayload(raw: Record<string, unknown>): Record<string, unknown> {
  if (typeof raw.a2ui === 'object' && raw.a2ui !== null) {
    return raw.a2ui as Record<string, unknown>
  }
  return raw
}

/**
 * Normalize a raw protobuf/agent JSON object into a typed A2UI v0.9 envelope
 * that can be fed to {@link MessageProcessor.processMessages}.
 *
 * @returns The normalized envelope, or `null` if the object isn't a valid A2UI message.
 */
export function normalizeToA2uiMessage(raw: Record<string, unknown>): A2uiMessage | null {
  if (!isA2UIMessage(raw)) return null
  const o = unwrapA2UIPayload(raw)
  const version = 'v0.9' as const

  if (o.createSurface && typeof o.createSurface === 'object') {
    return { version, createSurface: o.createSurface } as A2uiMessage
  }
  if (o.updateComponents && typeof o.updateComponents === 'object') {
    return { version, updateComponents: o.updateComponents } as A2uiMessage
  }
  if (o.updateDataModel && typeof o.updateDataModel === 'object') {
    return { version, updateDataModel: o.updateDataModel } as A2uiMessage
  }
  if (o.deleteSurface && typeof o.deleteSurface === 'object') {
    return { version, deleteSurface: o.deleteSurface } as A2uiMessage
  }
  return null
}

/**
 * Get (or lazily create) the {@link MessageProcessor} for a scope.
 *
 * Called by {@link A2UISurface} to bind the processor to the renderer.
 */
export function getOrCreateA2uiProcessor(scopeKey: string): MessageProcessor<ComponentApi> {
  let p = processors.get(scopeKey)
  if (!p) {
    p = new MessageProcessor([vuetifyCatalog], (action) => forwardScopeAction(scopeKey, action))
    processors.set(scopeKey, p)
  }
  return p
}

/**
 * Reset processor and deduplication state for a scope.
 */
export function resetA2uiProcessorScope(scopeKey: string): void {
  processors.delete(scopeKey)
  processedEnvelopeKeys.delete(scopeKey)
}

/**
 * Feed normalized A2UI payload into the scope processor at most once for `dedupeKey`.
 *
 * Accepts either one envelope object or an array of envelope objects.
 */
export function ingestA2uiPayloadOnce(scopeKey: string, dedupeKey: string, payload: unknown): void {
  if (!scopeKey) return
  if (!isA2UIMessage(payload)) return

  const processed = getProcessedSet(scopeKey)
  // Caller supplies stable keys (history: messageId:index, live: AG-UI message id).
  if (processed.has(dedupeKey)) return

  const processor = getOrCreateA2uiProcessor(scopeKey)
  const envelopes: A2uiMessage[] = []

  if (Array.isArray(payload)) {
    for (const item of payload) {
      const envelope = normalizeToA2uiMessage(item as Record<string, unknown>)
      if (envelope) envelopes.push(envelope)
    }
  } else {
    const envelope = normalizeToA2uiMessage(payload as Record<string, unknown>)
    if (envelope) envelopes.push(envelope)
  }

  if (envelopes.length === 0) return

  try {
    processor.processMessages(envelopes)
    // Mark key only after success so a failed parse can be retried on the next ingest.
    processed.add(dedupeKey)
  } catch (e) {
    console.error('[A2UI] processMessages failed', e)
  }
}
