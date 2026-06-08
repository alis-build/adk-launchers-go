/**
 * AG-UI chat streaming composable.
 *
 * @module pages/threads/composables/useChatStream
 */
import { getA2UICreateSurfaceId, useA2UISettingsStore } from '@/a2ui'
import { getAdkInvocationId } from '@/components/chat/toolInterruptDisplay'
import { createAguiAgent } from '@/pages/threads/clients'
import { useSelectedAgentStore } from '@/store/selectedAgent'
import { friendlyResumeRequiredMessage, isResumeRequiredRunError } from '@/pages/threads/hitlRunErrors'
import { clearThreadInterrupts, getThreadInterrupts, getUnresolvedInterruptState } from '@/pages/threads/store/messages'
import { formatError } from '@/utils/errorFormat'
import type { ADKAgent } from '@ag-ui/adk'
import type { AgentSubscriber } from '@ag-ui/client'
import type { ResumeEntry } from '@ag-ui/core'
import { CATALOG_ID, defaultRegistry, getCatalogSchema } from '@alis-build/a2ui-vuetify-renderer'
import { v7 as uuidv7 } from 'uuid'
import type { Ref } from 'vue'
import { ref, shallowRef } from 'vue'
import { aguiStreamDebugData } from '../store/aguiDebug'
import type { ChatMessage } from '../types'

/** A2UI schema forwarded to the agent via AG-UI `forwardedProps` when enabled. */
const a2uiForwardedProps = {
  a2uiSchema: getCatalogSchema(defaultRegistry, CATALOG_ID),
}

/** Per-thread ADKAgent cache so message history and state survive across sends. */
const agentInstances = new Map<string, ADKAgent>()

function agentCacheKey(threadId: string, agentId?: string): string {
  return agentId ? `${threadId}:${agentId}` : threadId
}

/** Returns the cached ADKAgent for a thread+agent pair, creating one if absent. */
function getOrCreateAgent(threadId: string, agentId?: string): ADKAgent {
  const key = agentCacheKey(threadId, agentId)
  let agent = agentInstances.get(key)
  if (!agent) {
    agent = createAguiAgent(threadId)
    agentInstances.set(key, agent)
  }
  return agent
}

/** Clears cached ADKAgent when a thread is deleted. */
export function clearAgentInstance(threadId: string) {
  for (const key of agentInstances.keys()) {
    if (key === threadId || key.startsWith(`${threadId}:`)) {
      agentInstances.delete(key)
    }
  }
}

/** Builds options for `agent.runAgent()`, optionally including A2UI and ADK invocation state. */
function runAgentOptions(a2uiEnabled: boolean, base: { abortController?: AbortController; resume?: ResumeEntry[]; adkInvocationId?: string }, threadAgentId?: string) {
  const agentStore = useSelectedAgentStore()
  const agentId = threadAgentId || agentStore.selectedAgentId
  const opts: {
    abortController?: AbortController
    resume?: ResumeEntry[]
    forwardedProps?: typeof a2uiForwardedProps
    state?: { adk: { invocationId: string } }
    context?: Array<{ description: string; value: string }>
  } = { ...base }
  if (agentId) {
    opts.context = [{ description: 'app', value: agentId }]
  }
  if (a2uiEnabled) {
    opts.forwardedProps = a2uiForwardedProps
  }
  if (base.adkInvocationId) {
    opts.state = { adk: { invocationId: base.adkInvocationId } }
  }
  return opts
}

/** ADKAgent.prepareRunAgentInput only sends agent.state; merge adk.invocationId for one resume run. */
function applyResumeInvocationState(agent: ADKAgent, invocationId: string | undefined): () => void {
  if (!invocationId) return () => {}
  const prev = structuredClone(agent.state ?? {}) as Record<string, unknown>
  const adk = (typeof prev.adk === 'object' && prev.adk !== null ? prev.adk : {}) as Record<string, unknown>
  agent.setState({ ...prev, adk: { ...adk, invocationId } })
  return () => agent.setState(prev)
}

/** Callback hooks invoked by the chat stream as events arrive. */
export interface ChatStreamCallbacks {
  /** Called for each finalised ChatMessage (text, tool call, status, A2UI surface). */
  onMessage: (msg: ChatMessage) => void
  /** Called when an A2UI surface payload is received from an activity snapshot. */
  onA2UI?: (payload: unknown, dedupeKey?: string) => void
  /** Called when the agent begins emitting reasoning/chain-of-thought. */
  onReasoningStart?: () => void
  /** Called for each incremental reasoning text delta. */
  onReasoningDelta?: (delta: string) => void
  /** Called when reasoning is complete, with the full accumulated text. */
  onReasoningEnd?: (fullText: string) => void
  /** Called when a stream error occurs. */
  onError?: (message: string) => void
  /** Called when the agent run finishes successfully. */
  onComplete?: () => void
}

/** Return type of the {@link useChatStream} composable. */
export interface UseChatStreamReturn {
  /** Whether an AG-UI stream is currently active. */
  isStreaming: Ref<boolean>
  /** Error message from the last failed stream, or `undefined` if none. */
  streamError: Ref<string | undefined>
  /** Sends a user text message and streams the agent's response. */
  send: (text: string, callbacks: ChatStreamCallbacks) => Promise<boolean>
  /** Sends an A2UI action (user interaction with a rendered surface) as a message. */
  sendA2UIAction: (actionText: string, callbacks: ChatStreamCallbacks) => Promise<void>
  /** Submits tool confirmation decisions and resumes the interrupted agent run. */
  submitInterruptResume: (resumeEntries: ResumeEntry[], callbacks: ChatStreamCallbacks) => Promise<void>
  /** Aborts the current stream. Safe to call when not streaming. */
  abort: () => void
}

/** True if the error was caused by an intentional stream abort (user clicked Stop). */
function isAbortedSendError(error: unknown, controller?: AbortController | null): boolean {
  if (controller?.signal.aborted) return true
  if (error instanceof DOMException && error.name === 'AbortError') return true
  const message = error instanceof Error ? error.message : String(error ?? '')
  return message.toLowerCase().includes('signal is aborted')
}

/** Guards send/action calls — fires an error callback and returns true when interrupts need resolution. */
function blockRunIfInterruptPending(threadId: string, callbacks: ChatStreamCallbacks): boolean {
  const pending = getUnresolvedInterruptState(threadId)
  if (!pending?.interrupts.length) return false
  callbacks.onError?.(friendlyResumeRequiredMessage(true))
  return true
}

/** Creates an AG-UI subscriber that translates SSE events into ChatMessages via callbacks. */
function buildSubscriber(callbacks: ChatStreamCallbacks, runTaskId: string, a2uiEnabled: boolean, threadId: string): AgentSubscriber {
  const textBuffers: Record<string, string> = {}
  const toolCallState: Record<string, { name: string; args: string }> = {}
  const emittedSurfaces = new Set<string>()
  let reasoningBuffer = ''

  return {
    onReasoningStartEvent() {
      reasoningBuffer = ''
      callbacks.onReasoningStart?.()
    },
    onReasoningMessageContentEvent({ event }) {
      reasoningBuffer += event.delta
      callbacks.onReasoningDelta?.(event.delta)
    },
    onReasoningEndEvent() {
      if (reasoningBuffer.trim()) {
        callbacks.onMessage({
          id: uuidv7(),
          role: 'assistant',
          content: '',
          reasoning: reasoningBuffer,
          payloadType: 'artifact_update',
          taskId: runTaskId,
          debugData: aguiStreamDebugData('reasoningEnd', { text: reasoningBuffer, taskId: runTaskId }),
        })
      }
      callbacks.onReasoningEnd?.(reasoningBuffer)
    },

    onTextMessageContentEvent({ event }) {
      textBuffers[event.messageId] = (textBuffers[event.messageId] ?? '') + event.delta
    },
    onTextMessageEndEvent({ event }) {
      const text = textBuffers[event.messageId] ?? ''
      delete textBuffers[event.messageId]
      if (text) {
        callbacks.onMessage({
          id: event.messageId,
          role: 'assistant',
          content: text,
          payloadType: 'message',
          debugData: aguiStreamDebugData('textMessageEnd', { event, text }),
        })
      }
    },

    onToolCallStartEvent({ event }) {
      toolCallState[event.toolCallId] = { name: event.toolCallName, args: '' }
    },
    onToolCallArgsEvent({ event }) {
      if (toolCallState[event.toolCallId]) {
        toolCallState[event.toolCallId].args += event.delta
      }
    },
    onToolCallEndEvent({ event }) {
      const tc = toolCallState[event.toolCallId]
      if (tc) {
        delete toolCallState[event.toolCallId]
        let parsedArgs: Record<string, unknown> = {}
        try {
          parsedArgs = JSON.parse(tc.args)
        } catch {
          // Malformed args should not fail the whole run.
        }
        callbacks.onMessage({
          id: event.toolCallId,
          role: 'assistant',
          content: '',
          toolCalls: [{ id: event.toolCallId, name: tc.name, args: parsedArgs }],
          payloadType: 'message',
          debugData: aguiStreamDebugData('toolCallEnd', { event, toolCall: tc, args: parsedArgs }),
        })
      }
    },
    onToolCallResultEvent({ event }) {
      callbacks.onMessage({
        id: event.messageId,
        role: 'assistant',
        content: '',
        toolResults: [{ toolCallId: event.toolCallId, content: event.content }],
        payloadType: 'message',
        debugData: aguiStreamDebugData('toolCallResult', { event }),
      })
    },

    onActivitySnapshotEvent({ event }) {
      if (!a2uiEnabled) return
      if (event.activityType === 'a2ui-surface') {
        const content = event.content as Record<string, unknown>
        const ops = content?.a2ui_operations
        if (ops) {
          callbacks.onA2UI?.(ops, event.messageId)
          const opsArray = Array.isArray(ops) ? ops : [ops]
          for (const op of opsArray) {
            const surfaceId = getA2UICreateSurfaceId(op as Record<string, unknown>)
            if (surfaceId && !emittedSurfaces.has(surfaceId)) {
              emittedSurfaces.add(surfaceId)
              callbacks.onMessage({
                id: `a2ui-surface-${surfaceId}`,
                role: 'assistant',
                content: '',
                a2uiPayloads: opsArray,
                payloadType: 'message',
                debugData: aguiStreamDebugData('activitySnapshot', { event, surfaceId, ops: opsArray }),
              })
            }
          }
        }
      }
    },

    onRunErrorEvent({ event }) {
      const raw = event.message ?? 'Agent error'
      const msg = isResumeRequiredRunError(raw) ? friendlyResumeRequiredMessage(!!getUnresolvedInterruptState(threadId)) : raw
      if (!isResumeRequiredRunError(raw)) {
        callbacks.onMessage({
          id: uuidv7(),
          role: 'assistant',
          content: msg,
          status: 'failed',
          payloadType: 'status_update',
          taskId: runTaskId,
          debugData: aguiStreamDebugData('runError', { event, taskId: runTaskId }),
        })
      }
      callbacks.onError?.(msg)
    },

    onRunFinishedEvent(params) {
      if (params.outcome === 'interrupt') {
        const toolInterrupts = (params.interrupts ?? []).filter((i) => i.reason === 'tool_call')
        if (toolInterrupts.length === 0) {
          callbacks.onError?.('Interrupt run had no tool_call interrupts')
          callbacks.onMessage({
            id: uuidv7(),
            role: 'assistant',
            content: 'Invalid interrupt data received',
            status: 'failed',
            payloadType: 'status_update',
            taskId: runTaskId,
            debugData: aguiStreamDebugData('runFinishedInterruptError', { event: params.event }),
          })
          return
        }

        callbacks.onMessage({
          id: uuidv7(),
          role: 'assistant',
          content: '',
          payloadType: 'interrupt',
          interrupts: toolInterrupts,
          runId: params.event.runId,
          taskId: runTaskId,
          timestamp: Date.now(),
          debugData: aguiStreamDebugData('runFinishedInterrupt', {
            event: params.event,
            interrupts: toolInterrupts,
          }),
        })

        callbacks.onMessage({
          id: uuidv7(),
          role: 'assistant',
          content: '',
          status: 'awaiting_confirmation',
          payloadType: 'status_update',
          taskId: runTaskId,
          debugData: aguiStreamDebugData('awaitingConfirmation', { runId: params.event.runId }),
        })
        return
      }

      callbacks.onMessage({
        id: uuidv7(),
        role: 'assistant',
        content: '',
        status: 'completed',
        payloadType: 'status_update',
        taskId: runTaskId,
        debugData: aguiStreamDebugData('runFinished', { event: params.event, taskId: runTaskId }),
      })
      clearThreadInterrupts(threadId)
      callbacks.onComplete?.()
    },
  }
}

/** Vue composable for AG-UI chat streaming — send, resume, and abort agent runs. */
export function useChatStream(threadId: Ref<string>, threadAgentId?: Ref<string | undefined>): UseChatStreamReturn {
  const a2uiSettingsStore = useA2UISettingsStore()
  const isStreaming = ref(false)
  const streamError = ref<string | undefined>(undefined)
  let abortController = shallowRef<AbortController | null>(null)

  function abort() {
    abortController.value?.abort()
  }

  async function send(text: string, callbacks: ChatStreamCallbacks): Promise<boolean> {
    const tid = threadId.value
    if (!tid || isStreaming.value) return false
    if (blockRunIfInterruptPending(tid, callbacks)) return false
    isStreaming.value = true
    streamError.value = undefined
    const controller = new AbortController()
    abortController.value = controller
    const runTaskId = uuidv7()

    callbacks.onMessage({
      id: runTaskId,
      role: 'assistant',
      content: '',
      status: 'working',
      payloadType: 'status_update',
      taskId: runTaskId,
      debugData: aguiStreamDebugData('statusUpdate', { status: 'working', taskId: runTaskId }),
    })

    try {
      const agent = getOrCreateAgent(tid, threadAgentId?.value)
      agent.addMessage({ id: uuidv7(), role: 'user', content: text })
      const a2uiOn = a2uiSettingsStore.effectiveA2uiEnabled
      const subscriber = buildSubscriber(callbacks, runTaskId, a2uiOn, tid)
      await agent.runAgent(runAgentOptions(a2uiOn, { abortController: controller }, threadAgentId?.value), subscriber)
      return true
    } catch (e) {
      if (isAbortedSendError(e, controller)) return false
      const msg = formatError(e, 'Failed to send message')
      streamError.value = msg
      callbacks.onMessage({
        id: uuidv7(),
        role: 'assistant',
        content: msg,
        status: 'failed',
        payloadType: 'status_update',
        taskId: runTaskId,
        debugData: aguiStreamDebugData('runError', { message: msg, taskId: runTaskId, error: String(e) }),
      })
      callbacks.onError?.(msg)
      return false
    } finally {
      abortController.value = null
      isStreaming.value = false
    }
  }

  async function sendA2UIAction(actionText: string, callbacks: ChatStreamCallbacks): Promise<void> {
    const tid = threadId.value
    if (!tid) return
    if (blockRunIfInterruptPending(tid, callbacks)) return
    isStreaming.value = true
    streamError.value = undefined
    const controller = new AbortController()
    abortController.value = controller
    const runTaskId = uuidv7()

    try {
      const agent = getOrCreateAgent(tid, threadAgentId?.value)
      agent.addMessage({ id: uuidv7(), role: 'user', content: actionText })
      const a2uiOn = a2uiSettingsStore.effectiveA2uiEnabled
      const subscriber = buildSubscriber(callbacks, runTaskId, a2uiOn, tid)
      await agent.runAgent(runAgentOptions(a2uiOn, { abortController: controller }, threadAgentId?.value), subscriber)
    } catch (e) {
      if (isAbortedSendError(e, controller)) return
      const msg = formatError(e, 'Failed to send UI action')
      streamError.value = msg
      callbacks.onMessage({
        id: uuidv7(),
        role: 'assistant',
        content: msg,
        status: 'failed',
        payloadType: 'status_update',
        taskId: runTaskId,
        debugData: aguiStreamDebugData('runError', { message: msg, taskId: runTaskId, error: String(e) }),
      })
      callbacks.onError?.(msg)
    } finally {
      abortController.value = null
      isStreaming.value = false
    }
  }

  /** Finds the ADK invocation ID from pending interrupts so the server can match the resume. */
  function resolveAdkInvocationIdForResume(threadId: string): string | undefined {
    const pending = getUnresolvedInterruptState(threadId)?.interrupts ?? getThreadInterrupts(threadId)
    for (const intr of pending) {
      const id = getAdkInvocationId(intr)
      if (id) return id
    }
    return undefined
  }

  async function submitInterruptResume(resumeEntries: ResumeEntry[], callbacks: ChatStreamCallbacks): Promise<void> {
    const tid = threadId.value
    if (!tid || isStreaming.value) return

    isStreaming.value = true
    streamError.value = undefined
    const runTaskId = uuidv7()

    try {
      const agent = getOrCreateAgent(tid, threadAgentId?.value)
      const a2uiOn = a2uiSettingsStore.effectiveA2uiEnabled
      const adkInvocationId = resolveAdkInvocationIdForResume(tid)
      const restoreState = applyResumeInvocationState(agent, adkInvocationId)
      const subscriber = buildSubscriber(callbacks, runTaskId, a2uiOn, tid)
      try {
        await agent.runAgent(runAgentOptions(a2uiOn, { resume: resumeEntries, adkInvocationId }, threadAgentId?.value), subscriber)
      } finally {
        restoreState()
      }
    } catch (e) {
      if (isAbortedSendError(e)) return
      const msg = formatError(e, 'Failed to submit confirmation')
      streamError.value = msg
      callbacks.onError?.(msg)
    } finally {
      isStreaming.value = false
    }
  }

  return {
    isStreaming,
    streamError,
    send,
    sendA2UIAction,
    submitInterruptResume,
    abort,
  }
}
