import type { AgentSubscriber } from '@ag-ui/client'
import { setThreadInterrupts } from '@/pages/threads/store/messages'
import type { Interrupt, ResumeEntry } from '../types'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { ChatMessage } from '../types'

const runAgentMock = vi.fn()
const addMessageMock = vi.fn()
const setStateMock = vi.fn()

let agentState: Record<string, unknown> = {}

vi.mock('@/pages/threads/clients', () => ({
  createAguiAgent: () => ({
    addMessage: addMessageMock,
    runAgent: runAgentMock,
    get state() {
      return agentState
    },
    setState: setStateMock.mockImplementation((s: Record<string, unknown>) => {
      agentState = s
    }),
  }),
}))

const { clearAgentInstance, useChatStream } = await import('./useChatStream')

describe('useChatStream interrupt handling', () => {
  const threadId = ref('ctx-1')
  let capturedSubscriber: AgentSubscriber | undefined
  const received: ChatMessage[] = []

  beforeEach(() => {
    setActivePinia(createPinia())
    clearAgentInstance('ctx-1')
    runAgentMock.mockReset()
    addMessageMock.mockReset()
    setStateMock.mockClear()
    agentState = {}
    capturedSubscriber = undefined
    received.length = 0

    runAgentMock.mockImplementation(async (_opts: { resume?: ResumeEntry[] }, subscriber: AgentSubscriber) => {
      capturedSubscriber = subscriber
      if (_opts.resume) return
      subscriber.onRunFinishedEvent?.({
        outcome: 'interrupt',
        interrupts: [{
          id: 'intr-1',
          reason: 'tool_call',
          toolCallId: 'tc-1',
        } satisfies Interrupt],
        event: { runId: 'run-1' },
      })
    })
  })

  it('maps interrupt RunFinished to interrupt + awaiting_confirmation messages', async () => {
    const { send } = useChatStream(threadId)
    const callbacks = {
      onMessage: (msg: ChatMessage) => { received.push(msg) },
    }

    await send('hello', callbacks)

    expect(received.some((m) => m.payloadType === 'interrupt' && m.interrupts?.length === 1)).toBe(true)
    expect(received.some((m) => m.payloadType === 'status_update' && m.status === 'awaiting_confirmation')).toBe(true)
    expect(received.some((m) => m.status === 'completed')).toBe(false)
  })

  it('submitInterruptResume passes resume to runAgent with subscriber', async () => {
    const { send, submitInterruptResume } = useChatStream(threadId)
    const callbacks = { onMessage: vi.fn() }

    await send('hello', callbacks)

    const resume: ResumeEntry[] = [{
      interruptId: 'intr-1',
      status: 'resolved',
      payload: { approved: true },
    }]

    runAgentMock.mockClear()
    await submitInterruptResume(resume, callbacks)

    expect(runAgentMock).toHaveBeenCalledTimes(1)
    const [opts, subscriber] = runAgentMock.mock.calls[0]!
    expect(opts.resume).toEqual(resume)
    expect(subscriber).toBeDefined()
    expect(capturedSubscriber).toBe(subscriber)
  })

  it('submitInterruptResume sets agent state.adk.invocationId from pending interrupts', async () => {
    setThreadInterrupts('ctx-1', [{
      id: 'intr-1',
      reason: 'tool_call',
      metadata: { adk: { invocationId: 'e-resume-99' } },
    } satisfies Interrupt])

    const { submitInterruptResume } = useChatStream(threadId)
    const resume: ResumeEntry[] = [{
      interruptId: 'intr-1',
      status: 'resolved',
      payload: { approved: true },
    }]

    runAgentMock.mockImplementation(async () => {})
    await submitInterruptResume(resume, { onMessage: vi.fn() })

    expect(setStateMock).toHaveBeenCalled()
    const duringRun = setStateMock.mock.calls.find(
      (call) => (call[0] as { adk?: { invocationId?: string } }).adk?.invocationId === 'e-resume-99',
    )
    expect(duringRun).toBeDefined()
    expect(agentState).toEqual({})
  })
})
