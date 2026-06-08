/**
 * Chat message domain types.
 *
 * Normalized chat message model for the threads UI, produced from AG-UI
 * history messages and SSE stream events.
 *
 * @module pages/threads/types
 */
import type { Interrupt, ResumeEntry } from '@ag-ui/core'

export type { Interrupt, ResumeEntry }

/** Resume payload for tool_call interrupts (matches adk-launchers-go schema). */
export interface ToolConfirmationPayload {
  approved: boolean
  editedArgs?: Record<string, unknown>
}

/** Serializable AG-UI debug envelope for {@link ChatMessage.debugData}. */
export interface AGUIDebugPayload {
  source: 'agui'
  kind: string
  data: unknown
}

/**
 * A normalized chat message displayed in the conversation view.
 *
 * Produced from AG-UI history (`setMessagesFromAGUI`) or live SSE (`useChatStream`).
 */
export interface ChatMessage {
  /** Unique message identifier (event ID, message ID, or generated UUID). */
  id: string
  /** Whether the message is from the user or the agent. */
  role: 'user' | 'assistant'
  /** The text content of the message (may be empty for tool-only or A2UI messages). */
  content: string
  /** Tool calls made by the agent in this message (function invocations). */
  toolCalls?: ChatToolCall[]
  /** Tool results returned to the agent (function responses). */
  toolResults?: ChatToolResult[]
  /** Agent's internal reasoning / chain-of-thought text. */
  reasoning?: string
  /** Current task status for status_update payloads. */
  status?: ChatStatus
  /** A2UI surface payloads embedded in this message (createSurface, updateComponents, etc.). */
  a2uiPayloads?: unknown[]
  /** File attachments included in the message. */
  files?: ChatFile[]
  /** Message creation timestamp in milliseconds since epoch. */
  timestamp?: number
  /** Raw AG-UI history message or SSE event envelope for the debug dialog. */
  debugData?: AGUIDebugPayload
  /** Discriminant indicating how this message was derived (message, status, etc.). */
  payloadType?: 'message' | 'status_update' | 'artifact_update' | 'task' | 'interrupt'
  /** Open AG-UI interrupts when {@link payloadType} is `'interrupt'`. */
  interrupts?: Interrupt[]
  /** AG-UI run id for interrupt rows. */
  runId?: string
  /** The full ThreadEvent resource name (e.g. `threads/{id}/events/{eventId}`). */
  resourceName?: string
  /** The A2A task ID this message belongs to. */
  taskId?: string
  /** The A2A context (session) ID for this message. */
  contextId?: string
  /** Metadata from the artifact update envelope (e.g. artifact type, name). */
  artifactEnvelopeMetadata?: Record<string, unknown>
}

/**
 * Represents a single tool (function) call made by the agent.
 */
export interface ChatToolCall {
  /** Tool call identifier used to correlate with the result. */
  id: string
  /** The name of the tool/function being called. */
  name: string
  /** The arguments passed to the tool as key-value pairs. */
  args: Record<string, unknown>
  /** The tool's return value (populated when the result arrives). */
  result?: unknown
  /** Whether the tool call completed, errored, or is still awaiting confirmation. */
  status?: 'completed' | 'error' | 'pending'
}

/**
 * Represents the response from a tool call back to the agent.
 */
export interface ChatToolResult {
  /** The ID of the tool call this result belongs to. */
  toolCallId: string
  /** The serialized result content (typically JSON). */
  content: string
}

/**
 * A file attachment within a chat message.
 */
export interface ChatFile {
  /** Remote URL to fetch the file from. */
  url?: string
  /** Raw file bytes (for inline/embedded files). */
  bytes?: Uint8Array
  /** Display name of the file. */
  filename: string
  /** MIME type of the file content. */
  mimeType: string
}

/**
 * Possible states for an agent task in the threads UI.
 */
export type ChatStatus = 'working' | 'completed' | 'failed' | 'awaiting_confirmation'

/** Result of a JSON or form validation check. */
export interface ValidationResult {
  valid: boolean
  error?: string
}

/**
 * Returns a human-readable label for a message's payload type.
 */
export function getPayloadTypeLabel(type: ChatMessage['payloadType']): string {
  switch (type) {
    case 'message': return 'Message'
    case 'status_update': return 'Status update'
    case 'task': return 'Task'
    case 'artifact_update': return 'Artifact'
    case 'interrupt': return 'Interrupt'
    default: return 'Unknown'
  }
}

/**
 * Returns a user-facing description of a task status.
 */
export function getStateText(status: ChatStatus | undefined): string | undefined {
  switch (status) {
    case 'working': return 'Task in progress'
    case 'completed': return 'Task completed'
    case 'failed': return 'Task failed'
    case 'awaiting_confirmation': return 'Confirmation required'
    default: return undefined
  }
}

/**
 * Checks whether a task status represents a terminal state.
 */
export function isFinalStatus(status: ChatStatus | undefined): boolean {
  return status === 'completed' || status === 'failed'
}

/**
 * Returns a display label for the message sender role.
 */
export function getRoleLabel(msg: ChatMessage): 'User' | 'Agent' | undefined {
  if (msg.payloadType !== 'message') return undefined
  return msg.role === 'user' ? 'User' : 'Agent'
}
