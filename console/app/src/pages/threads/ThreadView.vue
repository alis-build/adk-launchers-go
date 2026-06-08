<script setup lang="ts">
  import { a2uiSendActionKey, buildA2UIActionPart, ingestA2uiPayloadOnce, resetA2uiProcessorScope, setA2uiActionHandler, useA2UISettingsStore } from '@/a2ui'
  import AgentStatusIndicator from '@/components/chat/AgentStatusIndicator.vue'
  import ChatBubble from '@/components/chat/ChatBubble.vue'
  import { statusHintForInterrupts } from '@/components/chat/toolInterruptDisplay'
  import ToolInterruptResolver from '@/components/chat/ToolInterruptResolver.vue'
  import type { MessagePart } from '@/components/ContentInput/types'
  import { deleteThread as deleteThreadREST, fetchThread, fetchThreadMessages, updateUserThreadState } from '@/pages/threads/clients'
  import ThreadComposerCard from '@/pages/threads/components/ThreadComposerCard.vue'
  import ThreadMessageDebugDialog from '@/pages/threads/components/ThreadMessageDebugDialog.vue'
  import ThreadMessageDetailsDialog from '@/pages/threads/components/ThreadMessageDetailsDialog.vue'
  import { clearAgentInstance, useChatStream } from '@/pages/threads/composables/useChatStream'
  import { processConversationA2uiMessages } from '@/pages/threads/store/a2uiConversationFeed'
  import { aguiUserInputDebugData } from '@/pages/threads/store/aguiDebug'
  import { friendlyResumeRequiredMessage, isResumeRequiredRunError } from '@/pages/threads/hitlRunErrors'
  import { addMessage, appendMessagesFromAGUI, clearMessages, getMessages, getUnresolvedInterruptState, setMessagesFromAGUI, setThreadInterrupts } from '@/pages/threads/store/messages'
  import type { ThreadListItem } from '@/pages/threads/store/threads'
  import { addThread, getThreadResumeSequence, getThreads, removeThread, setThreadTitleLoading, updateThread } from '@/pages/threads/store/threads'
  import { useThreadViewDialogStore } from '@/pages/threads/store/threadViewDialog'
  import type { ChatMessage, ChatStatus, ChatToolCall, Interrupt, ResumeEntry } from '@/pages/threads/types'
  import { buildThreadName } from '@/pages/threads/utils'
  import { getRuntimeConfig } from '@/runtimeConfig'
  import { useAppStore } from '@/store/app'
  import { useSelectedAgentStore } from '@/store/selectedAgent'
  import { useSnackbarStore } from '@/store/snackbar'
  import { useThreadComposerStore } from '@/store/threadComposer'
  import { formatError, logError } from '@/utils/errorFormat'
  import type { A2uiClientAction } from '@a2ui/web_core/v0_9'
  import { v7 as uuidv7 } from 'uuid'
  import type { Ref } from 'vue'
  import { computed, nextTick, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
  import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'

  const route = useRoute()
  const router = useRouter()
  const appStore = useAppStore()
  const snackbarStore = useSnackbarStore()
  const threadComposerStore = useThreadComposerStore()
  const threadViewDialogStore = useThreadViewDialogStore()
  const a2uiSettingsStore = useA2UISettingsStore()
  const agentStore = useSelectedAgentStore()
  const threadId = computed(() => (route.params.threadId as string) ?? '')

  const loadingListEvents: Ref<boolean> = ref(false)
  const errorListEvents: Ref<string | undefined> = ref(undefined)
  const loadingSendMessage: Ref<boolean> = ref(false)
  const errorSendMessage: Ref<string | undefined> = ref(undefined)
  const submittingResume: Ref<boolean> = ref(false)
  const messagesContainer: Ref<HTMLElement | null> = ref(null)
  const refreshingThreadTitle: Ref<boolean> = ref(false)
  const threadActionsMenuOpen: Ref<boolean> = ref(false)
  const deletingThread: Ref<boolean> = ref(false)
  const pendingResumeScrollThreadId: Ref<string | undefined> = ref(undefined)
  const isReasoning: Ref<boolean> = ref(false)
  const reasoningText: Ref<string> = ref('')
  const nextCursor: Ref<string | undefined> = ref(undefined)
  const loadingMoreMessages: Ref<boolean> = ref(false)

  const PAGE_SIZE = 50

  const currentThread = computed(() => getThreads().find((thread) => thread.name === buildThreadName(threadId.value)))
  const threadTitle = computed(() => currentThread.value?.displayName?.trim() || 'Thread')
  const threadAgentId = computed(() => currentThread.value?.agentId || agentStore.selectedAgentId || undefined)
  const { send: streamSend, sendA2UIAction: streamSendA2UIAction, submitInterruptResume: streamSubmitInterruptResume, abort: streamAbort } = useChatStream(threadId, threadAgentId)
  const traceProjectId = (getRuntimeConfig().gcpProject ?? '').trim()

  function buildOptimisticThread(contextId: string, _parts: MessagePart[]): ThreadListItem {
    return {
      name: buildThreadName(contextId),
      displayName: '',
      agentId: '',
      agentDisplayName: '',
      runCount: 0,
      readRunCount: 0,
      hasUnread: false,
      pinned: false,
    }
  }

  async function markThreadRead(threadName: string): Promise<void> {
    const existingThread = getThreads().find((thread) => thread.name === threadName)
    if (!existingThread || !existingThread.hasUnread) return

    try {
      await updateUserThreadState({
        userThreadState: {
          thread: threadName,
          readRunCount: existingThread.runCount,
        },
        updateMask: 'read_run_count',
      })

      updateThread({
        ...existingThread,
        readRunCount: existingThread.runCount,
        hasUnread: false,
      })
    } catch (e) {
      logError('updateUserThreadState', e)
    }
  }

  async function syncViewedThreadState(threadName: string): Promise<void> {
    const local = getThreads().find((t) => t.name === threadName)
    // Optimistic new thread: server has no metadata yet; skip fetch/read until after first run.
    if (local && local.runCount === 0 && !local.displayName?.trim()) {
      return
    }

    const contextId = threadName.replace(/^threads\//, '')
    try {
      const thread = await fetchThread(contextId)
      updateThread(thread)
      await markThreadRead(threadName)
    } catch (e) {
      logError('syncViewedThreadState', e)
    }
  }

  /** Loads the first page of AG-UI history and resets A2UI processor state for the thread. */
  async function fetchEvents() {
    const tid = threadId.value
    if (!tid) return

    // Arm messageItems watcher: after history renders, scroll to last-read sequence (not bottom).
    pendingResumeScrollThreadId.value = tid
    loadingListEvents.value = true
    appStore.setGlobalThreadLoading(true)
    errorListEvents.value = undefined

    // Full reload; do not append with a stale cursor.
    nextCursor.value = undefined
    try {
      const res = await fetchThreadMessages(tid, { limit: PAGE_SIZE, agentId: currentThread.value?.agentId || agentStore.selectedAgentId || undefined })

      // Before replay — drop processor + dedupe from a prior visit or thread switch.
      resetA2uiProcessorScope(tid)

      // Hydrate store; chatMessages watch runs processConversationA2uiMessages for history.
      setMessagesFromAGUI(tid, res.messages)
      nextCursor.value = res.nextCursor || undefined
    } catch (e) {
      logError('fetchThreadMessages', e)
      const msg = formatError(e, 'Failed to load thread messages')
      errorListEvents.value = msg
      snackbarStore.error(msg, { timeout: msg.length > 160 ? 20000 : 8000 })
    } finally {
      loadingListEvents.value = false
      appStore.setGlobalThreadLoading(false)
    }
  }

  async function loadMoreMessages() {
    const tid = threadId.value
    const cursor = nextCursor.value
    if (!tid || !cursor || loadingMoreMessages.value) return
    loadingMoreMessages.value = true
    try {
      const res = await fetchThreadMessages(tid, { after: cursor, limit: PAGE_SIZE, agentId: currentThread.value?.agentId || agentStore.selectedAgentId || undefined })
      if (res.messages.length > 0) {
        // Append only; processor already has state from initial fetch — do not reset scope.
        appendMessagesFromAGUI(tid, res.messages)
      }
      // Empty page means end of history; clear cursor so we stop prefetching.
      nextCursor.value = res.messages.length > 0 ? res.nextCursor || undefined : undefined
    } catch (e) {
      logError('loadMoreMessages', e)
    } finally {
      loadingMoreMessages.value = false
    }
  }

  const chatMessages = computed(() => getMessages(threadId.value ?? '') ?? [])

  watch(
    chatMessages,
    (msgs) => {
      const tid = threadId.value
      // History + live store updates: replay A2UI envelopes into the thread-scoped processor.
      if (tid && a2uiSettingsStore.effectiveA2uiEnabled) {
        processConversationA2uiMessages(tid, msgs)
      }
    },
    { immediate: true },
  )

  function latestArtifactEnvelopeMetadataForTask(taskId: string): Record<string, unknown> | undefined {
    if (!taskId) return undefined
    let last: Record<string, unknown> | undefined
    for (const m of chatMessages.value) {
      if (m.payloadType !== 'artifact_update') continue
      if (m.taskId !== taskId) continue
      if (m.artifactEnvelopeMetadata) last = m.artifactEnvelopeMetadata
    }
    return last
  }

  const unresolvedInterruptState = computed(() => {
    const tid = threadId.value
    if (!tid) return null
    return getUnresolvedInterruptState(tid)
  })

  const pendingInterrupts = computed((): Interrupt[] => {
    const intr = unresolvedInterruptState.value?.interrupts
    return Array.isArray(intr) ? intr : []
  })

  const interruptStatusHint = computed(() => statusHintForInterrupts(pendingInterrupts.value.length))

  const composerPlaceholder = computed(() => (pendingInterrupts.value.length > 0 ? 'Waiting on confirmation…' : 'Enter a prompt for your agent'))

  const interruptToolCalls = computed((): ChatToolCall[] => {
    const msgs = chatMessages.value
    const calls: ChatToolCall[] = []
    for (const intr of pendingInterrupts.value) {
      if (!intr.toolCallId) continue
      for (let i = msgs.length - 1; i >= 0; i--) {
        const msg = msgs[i]
        if (msg?.payloadType !== 'message' || !msg.toolCalls) continue
        const match = msg.toolCalls.find((tc) => tc.id === intr.toolCallId)
        if (match) {
          calls.push(match)
          break
        }
      }
    }
    return calls
  })

  const hiddenToolCallIds = computed(() => {
    const ids = new Set<string>()
    for (const intr of pendingInterrupts.value) {
      if (intr.toolCallId) ids.add(intr.toolCallId)
    }
    return [...ids]
  })

  /** Tool-call-only rows are fully covered by ToolInterruptResolver; skip empty duplicate bubbles. */
  function isOnlyHiddenToolCallMessage(msg: ChatMessage, hiddenIds: string[]): boolean {
    if (!msg.toolCalls?.length) return false
    if (msg.content?.trim()) return false
    if (msg.a2uiPayloads?.length) return false
    if (msg.files?.length) return false
    const hidden = new Set(hiddenIds)
    return msg.toolCalls.every((tc) => hidden.has(tc.id))
  }

  const composerDisabled = computed(() => loadingSendMessage.value || submittingResume.value || pendingInterrupts.value.length > 0)

  function latestStatusOf(msgs: ChatMessage[]): ChatStatus {
    return msgs[msgs.length - 1]?.status ?? 'working'
  }

  function errorMessageOf(msgs: ChatMessage[]): string | undefined {
    const last = msgs[msgs.length - 1]
    if (last?.status !== 'failed') return undefined
    return last.content || undefined
  }

  interface MessageItem {
    key: string
    type: 'message' | 'status' | 'interrupt_resolver'
    message?: ChatMessage
    statusMessages?: ChatMessage[]
    thoughts?: string[]
    isUser?: boolean
    interrupts?: Interrupt[]
    toolCalls?: ChatToolCall[]
  }

  interface Turn {
    userMsg?: ChatMessage
    agentMsgs: ChatMessage[]
    statusMsgs: ChatMessage[]
  }

  const messageItems = computed((): MessageItem[] => {
    const msgs = chatMessages.value ?? []
    const items: MessageItem[] = []
    const pending = pendingInterrupts.value ?? []
    const unresolvedMessageId = unresolvedInterruptState.value?.messageId

    const turns: Turn[] = []
    let currentTurn: Turn = { agentMsgs: [], statusMsgs: [] }

    // One turn = user message plus following agent/status rows until the next user message.
    for (const msg of msgs) {
      if (msg.role === 'user' && msg.payloadType === 'message') {
        if (currentTurn.userMsg || currentTurn.agentMsgs.length > 0 || currentTurn.statusMsgs.length > 0) {
          turns.push(currentTurn)
        }
        currentTurn = { userMsg: msg, agentMsgs: [], statusMsgs: [] }
      } else if (msg.payloadType === 'status_update') {
        currentTurn.statusMsgs.push(msg)
      } else {
        currentTurn.agentMsgs.push(msg)
      }
    }
    if (currentTurn.userMsg || currentTurn.agentMsgs.length > 0 || currentTurn.statusMsgs.length > 0) {
      turns.push(currentTurn)
    }

    for (const turn of turns) {
      if (turn.userMsg) {
        items.push({
          key: turn.userMsg.resourceName || turn.userMsg.id,
          type: 'message',
          message: turn.userMsg,
          isUser: true,
        })
      }

      const turnHasPendingResolver = pending.length > 0
        && !!unresolvedMessageId
        && turn.agentMsgs.some((m) => m.id === unresolvedMessageId)

      const turnThoughts: string[] = []
      for (const m of turn.agentMsgs) {
        if (m.reasoning) turnThoughts.push(m.reasoning)
      }

      // Drop stale "working" once confirmation UI is active (avoids double star indicators).
      const preAgentStatus = turn.statusMsgs.filter((m) => {
        if (m.status === 'awaiting_confirmation') return false
        if (turnHasPendingResolver && m.status === 'working') return false
        return true
      })
      if (preAgentStatus.length > 0) {
        const taskId = preAgentStatus[0]?.taskId ?? turn.userMsg?.id ?? 'unknown'
        items.push({
          key: `status-${taskId}`,
          type: 'status',
          statusMessages: preAgentStatus,
          thoughts: turnThoughts,
          isUser: false,
        })
      }

      const hiddenIds = hiddenToolCallIds.value
      for (const agentMsg of turn.agentMsgs) {
        if (agentMsg.payloadType === 'interrupt') continue
        // Reasoning-only artifact rows have no visible bubble; status block already shows thoughts.
        if (agentMsg.payloadType === 'artifact_update' && !agentMsg.content && !agentMsg.a2uiPayloads?.length) {
          continue
        }
        if (pending.length > 0 && isOnlyHiddenToolCallMessage(agentMsg, hiddenIds)) {
          continue
        }
        items.push({
          key: agentMsg.resourceName || agentMsg.id,
          type: 'message',
          message: agentMsg,
          isUser: false,
        })
      }

      // Permission card after tool rows (prototype order): status hint, then resolver.
      if (turnHasPendingResolver) {
        const postAgentStatus = turn.statusMsgs.filter((m) => m.status === 'awaiting_confirmation')
        const confirmationStatus: ChatMessage[] = postAgentStatus.length > 0
          ? postAgentStatus
          : [{
              id: `inferred-status-${unresolvedMessageId}`,
              role: 'assistant',
              content: '',
              status: 'awaiting_confirmation',
              payloadType: 'status_update',
              taskId: unresolvedMessageId!,
            }]

        items.push({
          key: `status-confirm-${unresolvedMessageId}`,
          type: 'status',
          statusMessages: confirmationStatus,
          thoughts: preAgentStatus.length > 0 ? [] : turnThoughts,
          isUser: false,
        })
        items.push({
          key: `interrupt-resolver-${unresolvedMessageId}`,
          type: 'interrupt_resolver',
          interrupts: pending,
          toolCalls: interruptToolCalls.value,
          isUser: false,
        })
      }
    }

    return items
  })

  const isWaitingForFirstResponse = computed(() => {
    if (!loadingSendMessage.value) return false
    const items = messageItems.value ?? []
    if (items.length === 0) return false
    return items.every((item) => item.isUser)
  })

  const isWaitingForResponse = computed(() => loadingSendMessage.value)

  const latestUserMessageKey = computed(() => {
    const items = messageItems.value ?? []
    for (let i = items.length - 1; i >= 0; i--) {
      const item = items[i]
      if (item?.isUser) return item.key
    }
    return undefined
  })

  const hasAgentResponse = computed(() => (messageItems.value ?? []).some((item) => !item.isUser))
  const resumeSequence = computed(() => getThreadResumeSequence(currentThread.value))

  async function refreshThreadTitle(retries = 2): Promise<void> {
    const tid = threadId.value
    if (!tid || refreshingThreadTitle.value) return

    const threadName = buildThreadName(tid)
    const currentThread = getThreads().find((thread) => thread.name === threadName)
    if (!currentThread || currentThread.displayName?.trim()) {
      setThreadTitleLoading(threadName, false)
      return
    }

    refreshingThreadTitle.value = true
    try {
      const thread = await fetchThread(tid)
      updateThread(thread)
      const title = String(thread.displayName ?? thread.display_name ?? '')
      if (title.trim()) {
        setThreadTitleLoading(threadName, false)
      }
    } catch (e) {
      // Agent may set display_name shortly after first response; retry before giving up.
      if (retries > 0) {
        refreshingThreadTitle.value = false
        setTimeout(() => void refreshThreadTitle(retries - 1), 2000)
        return
      }
      logError('getThread', e)
    } finally {
      refreshingThreadTitle.value = false
    }
  }

  /** Wires {@link useChatStream} events into the messages store and A2UI processor. */
  function buildStreamCallbacks(tid: string) {
    return {
      onMessage: (msg: ChatMessage) => {
        addMessage(tid, msg)
      },
      onA2UI: a2uiSettingsStore.effectiveA2uiEnabled
        ? (payload: unknown, dedupeKey?: string) => {
            // Live stream: dedupe by AG-UI message id when present; history uses message key:index.
            ingestA2uiPayloadOnce(tid, dedupeKey ?? `agui-a2ui-${uuidv7()}`, payload)
          }
        : undefined,
      onReasoningStart: () => {
        isReasoning.value = true
        reasoningText.value = ''
      },
      onReasoningDelta: (delta: string) => {
        reasoningText.value += delta
      },
      onReasoningEnd: () => {
        isReasoning.value = false
      },
      onError: (msg: string) => {
        const tid = threadId.value
        if (isResumeRequiredRunError(msg) && tid) {
          const state = getUnresolvedInterruptState(tid)
          if (state) {
            setThreadInterrupts(tid, state.interrupts)
          }
          if (pendingInterrupts.value.length > 0) {
            return
          }
          const friendly = friendlyResumeRequiredMessage(false)
          errorSendMessage.value = friendly
          snackbarStore.error(friendly, { timeout: 10000 })
          return
        }
        errorSendMessage.value = msg
        snackbarStore.error(msg, { timeout: msg.length > 160 ? 20000 : 8000 })
      },
    }
  }

  async function sendMessage(parts: MessagePart[]) {
    const tid = threadId.value
    if (!tid || loadingSendMessage.value || pendingInterrupts.value.length > 0) return false
    loadingSendMessage.value = true
    errorSendMessage.value = undefined

    try {
      const userText = parts.map((p) => (p.type === 'text' ? (p as import('@/components/ContentInput/types').TextMessagePart).content : `[${p.type}]`)).join(' ')
      const userMsg: ChatMessage = {
        id: uuidv7(),
        role: 'user',
        content: userText,
        payloadType: 'message',
        timestamp: Date.now(),
        debugData: aguiUserInputDebugData({ text: userText, parts }),
      }
      addMessage(tid, userMsg)
      await nextTick()
      // Pin the new user bubble while the agent streams (see isWaitingForResponse watchers).
      scrollLatestUserMessageToTop()

      isReasoning.value = false
      reasoningText.value = ''

      const callbacks = buildStreamCallbacks(tid)
      const sent = await streamSend(userText, callbacks)
      if (sent) {
        await syncViewedThreadState(buildThreadName(tid))
        if (hasAgentResponse.value) {
          await refreshThreadTitle()
        }
      }
      return sent
    } catch (e) {
      logError('sendMessage', e)
      errorSendMessage.value = formatError(e, 'Failed to send message')
      snackbarStore.error(errorSendMessage.value, {
        timeout: errorSendMessage.value.length > 160 ? 20000 : 8000,
      })
      return false
    } finally {
      loadingSendMessage.value = false
      await nextTick()
      // After stream ends, follow full transcript (resume logic may override via messageItems watch).
      scrollToBottom()
    }
  }

  function stopGenerating() {
    streamAbort()
  }

  watch(
    () => [threadId.value, route.query.is_new_thread] as const,
    async ([tid, isNew]) => {
      if (!tid || typeof tid !== 'string') return
      if (isNew === 'true') {
        // Landed from composer with ?is_new_thread: send pending parts, no history fetch.
        pendingResumeScrollThreadId.value = undefined
        nextCursor.value = undefined
        resetA2uiProcessorScope(tid)
        clearMessages(tid)
        await nextTick()
        const pending = threadComposerStore.consumePendingParts()
        if (pending && pending.length > 0) {
          const optimisticThread = buildOptimisticThread(tid, pending)
          addThread(optimisticThread)
          setThreadTitleLoading(optimisticThread.name, true)

          const sent = await sendMessage(pending)
          if (sent) {
            if (hasAgentResponse.value) {
              await refreshThreadTitle()
            }
          }
        }
        // Drop ?is_new_thread so refresh does not re-send the first message.
        router.replace({ path: route.path, query: {} })
      } else if (getMessages(tid).length === 0) {
        fetchEvents()
      } else {
        // Messages still in store from sidebar navigation: skip refetch, restore scroll only.
        void markThreadRead(buildThreadName(tid))
        pendingResumeScrollThreadId.value = tid
        await nextTick()
        scrollToResumeSequence()
        pendingResumeScrollThreadId.value = undefined
      }
    },
    { immediate: true },
  )

  watch(
    () => currentThread.value?.agentId,
    (agentId) => {
      if (agentId) {
        agentStore.setSelectedAgentId(agentId)
      }
    },
  )

  async function onSubmitInterruptResume(resumeEntries: ResumeEntry[]) {
    const tid = threadId.value
    if (!tid || submittingResume.value) return
    submittingResume.value = true
    errorSendMessage.value = undefined
    const threadResourceName = buildThreadName(tid)
    try {
      const callbacks = buildStreamCallbacks(tid)
      await streamSubmitInterruptResume(resumeEntries, callbacks)
      await syncViewedThreadState(threadResourceName)
    } finally {
      submittingResume.value = false
      await nextTick()
      scrollToBottom()
    }
  }

  async function sendA2UIAction(action: A2uiClientAction) {
    if (!a2uiSettingsStore.effectiveA2uiEnabled) return
    if (!action || typeof (action as { name?: unknown }).name !== 'string' || !(action as { name: string }).name.trim()) {
      throw new Error('Invalid A2UI action payload: missing action name')
    }

    const tid = threadId.value
    if (!tid) return
    if (pendingInterrupts.value.length > 0) return
    loadingSendMessage.value = true
    errorSendMessage.value = undefined
    try {
      const actionText = await buildA2UIActionPart(action)
      const callbacks = buildStreamCallbacks(tid)
      await streamSendA2UIAction(actionText, callbacks)
    } catch (e) {
      logError('sendA2UIAction', e)
      errorSendMessage.value = formatError(e, 'Failed to send UI action')
      snackbarStore.error(errorSendMessage.value, {
        timeout: errorSendMessage.value.length > 160 ? 20000 : 8000,
      })
    } finally {
      loadingSendMessage.value = false
      await nextTick()
      scrollToBottom()
    }
  }

  provide(a2uiSendActionKey, sendA2UIAction)

  let unregisterA2uiActionHandler: (() => void) | undefined
  watch(
    threadId,
    (tid) => {
      unregisterA2uiActionHandler?.()
      unregisterA2uiActionHandler = undefined
      if (!tid) return
      // A2UI surfaces resolve actions against the active thread scope key.
      unregisterA2uiActionHandler = setA2uiActionHandler(tid, (action) => {
        void sendA2UIAction(action)
      })
    },
    { immediate: true },
  )
  onBeforeUnmount(() => {
    unregisterA2uiActionHandler?.()
  })

  const scrollToBottom = () => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  }

  const scrollToTop = () => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = 0
    }
  }

  const scrollLatestUserMessageToTop = () => {
    if (!messagesContainer.value) return
    // Keep the latest user message near the top while the agent response grows below.
    const latestRow = latestUserMessageKey.value
      ? messagesContainer.value.querySelector<HTMLElement>(`[data-message-key="${latestUserMessageKey.value}"]`)
      : Array.from(messagesContainer.value.querySelectorAll<HTMLElement>('.message-row')).at(-1)
    if (!latestRow) {
      scrollToTop()
      return
    }

    latestRow.scrollIntoView({ block: 'start', behavior: 'auto' })
    messagesContainer.value.scrollTop = Math.max(0, messagesContainer.value.scrollTop - 24)
  }

  const scrollToResumeSequence = () => {
    if (!messagesContainer.value) return false

    const sequence = resumeSequence.value
    if (sequence === undefined) {
      scrollToBottom()
      return true
    }

    // gRPC read state stores last viewed message sequence on the thread resource.
    const target = messagesContainer.value.querySelector<HTMLElement>(`[data-message-sequence="${sequence.toString()}"]`)
    if (!target) {
      scrollToBottom()
      return false
    }

    target.scrollIntoView({ block: 'start', behavior: 'auto' })
    messagesContainer.value.scrollTop = Math.max(0, messagesContainer.value.scrollTop - 24)
    return true
  }

  watch(messageItems, async () => {
    await nextTick()
    // Priority: resume after history load, then pin user bubble while streaming, else follow tail.
    if (pendingResumeScrollThreadId.value === threadId.value) {
      scrollToResumeSequence()
      pendingResumeScrollThreadId.value = undefined
      return
    }
    if (isWaitingForResponse.value) {
      scrollLatestUserMessageToTop()
      return
    }
    scrollToBottom()
  })

  watch(isWaitingForResponse, async (waiting) => {
    if (!waiting) return
    await nextTick()
    scrollLatestUserMessageToTop()
  })

  watch(isWaitingForFirstResponse, async (waiting) => {
    if (!waiting) return
    await nextTick()
    // Brand-new thread: only user row visible — keep viewport at top until agent content arrives.
    scrollToTop()
  })

  watch(
    () => [threadId.value, hasAgentResponse.value] as const,
    async ([tid, hasResponse]) => {
      if (!tid || !hasResponse) return
      if (loadingSendMessage.value) return
      await refreshThreadTitle()
    },
    { immediate: true },
  )

  function onMessagesScroll() {
    const el = messagesContainer.value
    if (!el || !nextCursor.value || loadingMoreMessages.value) return
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    // Prefetch older messages when the user scrolls near the bottom (chat loads newest first).
    if (distanceFromBottom < 200) {
      void loadMoreMessages()
    }
  }

  onMounted(async () => {
    await nextTick()
    scrollToBottom()
    messagesContainer.value?.addEventListener('scroll', onMessagesScroll, { passive: true })
  })

  onBeforeUnmount(() => {
    messagesContainer.value?.removeEventListener('scroll', onMessagesScroll)
  })

  function selectMessage(message: ChatMessage) {
    const envelope = message.artifactEnvelopeMetadata ?? latestArtifactEnvelopeMetadataForTask(message.taskId ?? '')
    threadViewDialogStore.openDetailsDialogForMessage(message, envelope)
  }

  function debugMessage(message: ChatMessage) {
    threadViewDialogStore.openDebugDialogForMessage(message)
  }

  function invocationIdForMessage(message: ChatMessage): string | undefined {
    const envelope = message.artifactEnvelopeMetadata ?? latestArtifactEnvelopeMetadataForTask(message.taskId ?? '')
    const invocationId = envelope?.adk_invocation_id
    return typeof invocationId === 'string' && invocationId ? invocationId : undefined
  }

  function traceHrefForMessage(message: ChatMessage): string | undefined {
    const invocationId = invocationIdForMessage(message)
    if (!invocationId || !traceProjectId) return undefined

    // Cloud Trace explorer heatmap filtered by Vertex agent invocation id (ADK metadata).
    const query = {
      plotType: 'HEATMAP',
      pointConnectionMethod: 'GAP_DETECTION',
      targetAxis: 'Y1',
      traceQuery: {
        resourceContainer: `projects/${traceProjectId}/locations/global/traceScopes/_Default`,
        spanDataValue: 'SPAN_DURATION',
        spanFilters: {
          apphubServices: [],
          apphubWorkloads: [],
          applicationIds: [],
          attributes: [
            {
              key: 'gcp.vertex.agent.invocation_id',
              value: [invocationId],
            },
          ],
          isRootSpan: false,
          kinds: [],
          maxDuration: '',
          minDuration: '',
          services: [],
          status: [],
        },
      },
    }

    return `https://console.cloud.google.com/traces/explorer;query=${encodeURIComponent(JSON.stringify(query))};duration=PT1H?project=${encodeURIComponent(traceProjectId)}`
  }

  async function handleDeleteThread(): Promise<void> {
    const tid = threadId.value
    if (!tid || deletingThread.value) return

    const threadName = buildThreadName(tid)
    deletingThread.value = true
    threadActionsMenuOpen.value = false

    try {
      await deleteThreadREST(tid)
      removeThread(threadName)
      clearAgentInstance(tid)
      clearMessages(tid)
      snackbarStore.success('Thread deleted.')
      await router.push('/threads')
    } catch (e) {
      logError('deleteThread', e)
      const msg = formatError(e, 'Failed to delete thread')
      snackbarStore.error(msg, { timeout: msg.length > 160 ? 20000 : 8000 })
    } finally {
      deletingThread.value = false
    }
  }

  onBeforeRouteLeave((_to, _from, next) => {
    threadViewDialogStore.resetDialogState()
    next()
  })
</script>

<template>
  <Teleport
    defer
    to="#app-bar-title"
  >
    <div class="thread-app-title text-title-medium text-truncate">
      {{ threadTitle }}
    </div>
  </Teleport>

  <Teleport
    defer
    to="#app-bar-actions"
  >
    <v-menu
      v-model="threadActionsMenuOpen"
      location="bottom end"
      offset="10"
      content-class="thread-actions-menu-content"
    >
      <template #activator="{ props: menuProps }">
        <v-btn
          icon
          variant="text"
          v-bind="menuProps"
          aria-label="Thread actions"
        >
          <v-icon>more_vert</v-icon>
        </v-btn>
      </template>

      <v-list
        density="compact"
        min-width="0"
        class="thread-actions-menu-list"
      >
        <v-list-item
          rounded="lg"
          class="thread-actions-menu-item"
          :disabled="deletingThread"
          @click.stop="handleDeleteThread"
        >
          <template #prepend>
            <v-icon>delete</v-icon>
          </template>
          <v-list-item-title>Delete thread</v-list-item-title>
        </v-list-item>
      </v-list>
    </v-menu>
  </Teleport>

  <div class="thread-view-page">
    <v-card
      rounded="xl"
      variant="flat"
      class="thread-view-shell d-flex flex-column h-100 overflow-hidden"
    >
      <div
        class="thread-view-chat"
        :class="{ 'thread-view-chat--thinking-first-turn': isWaitingForFirstResponse }"
      >
        <div
          ref="messagesContainer"
          class="messages-scroll"
          :class="{
            'messages-scroll--thinking-first-turn': isWaitingForFirstResponse,
            'messages-scroll--waiting-response': isWaitingForResponse,
          }"
        >
          <div
            v-for="item in messageItems ?? []"
            :key="item.key"
            :class="['message-row', { 'message-row-user': item.isUser, 'message-row-agent': !item.isUser }]"
            :data-message-key="item.key"
          >
            <ChatBubble
              v-if="item.type === 'message' && item.message"
              :message="item.message"
              :hidden-tool-call-ids="hiddenToolCallIds"
              :trace-href="traceHrefForMessage(item.message)"
              clickable
              @click="selectMessage"
              @debug="debugMessage"
            />
            <div
              v-else-if="item.type === 'status' && item.statusMessages"
              class="task-status-stack"
            >
              <AgentStatusIndicator
                :status="latestStatusOf(item.statusMessages)"
                :confirmation-hint="latestStatusOf(item.statusMessages) === 'awaiting_confirmation' ? interruptStatusHint : undefined"
                :thoughts="item.thoughts ?? []"
                :live-reasoning="latestStatusOf(item.statusMessages) === 'working' ? reasoningText : ''"
                :error-message="errorMessageOf(item.statusMessages)"
                :clickable="item.statusMessages.length > 0"
                @click="selectMessage(item.statusMessages[item.statusMessages.length - 1]!)"
              />
            </div>
            <ToolInterruptResolver
              v-else-if="item.type === 'interrupt_resolver' && item.interrupts?.length"
              :interrupts="item.interrupts"
              :tool-calls="item.toolCalls ?? []"
              :disabled="submittingResume"
              @submit-resume="onSubmitInterruptResume"
            />
          </div>

          <div
            v-if="loadingMoreMessages"
            class="d-flex justify-center py-4"
          >
            <v-progress-circular
              indeterminate
              size="24"
              width="2"
            />
          </div>

          <v-card
            v-if="(messageItems ?? []).length === 0 && !loadingListEvents"
            rounded="xl"
            variant="tonal"
            class="empty-state"
          >
            <v-card-text class="pa-8 text-center">
              <v-icon
                size="64"
                class="mb-4 text-medium-emphasis"
              >
                chat
              </v-icon>
              <div class="text-headline-small mb-4">No messages yet</div>
              <div class="text-body-large text-medium-emphasis">Start the conversation by sending a message</div>
            </v-card-text>
          </v-card>

          <div
            v-if="isWaitingForResponse"
            class="waiting-scroll-spacer"
          ></div>
        </div>

        <v-sheet
          class="input-sticky"
          color="transparent"
        >
          <v-alert
            v-if="(pendingInterrupts ?? []).length > 0"
            type="warning"
            variant="tonal"
            density="compact"
            class="mb-2"
          >
            <div class="d-flex align-center flex-wrap gap-2">
              <span>Resolve the action(s) above before sending a new message.</span>
              <v-chip
                size="small"
                variant="outlined"
                color="warning"
                class="mx-2"
              >
                {{ (pendingInterrupts ?? []).length }} pending
              </v-chip>
            </div>
          </v-alert>
          <ThreadComposerCard
            :loading="loadingSendMessage || submittingResume"
            :disabled="composerDisabled"
            :placeholder="composerPlaceholder"
            @submit="sendMessage"
            @stop="stopGenerating"
          />
        </v-sheet>
      </div>
    </v-card>
  </div>

  <ThreadMessageDetailsDialog />
  <ThreadMessageDebugDialog />
</template>

<style lang="scss" scoped>
  .thread-view-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }

  .thread-view-shell {
    flex: 1 1 auto;
    width: 100%;
    min-height: 0;
  }

  .thread-view-chat {
    flex: 1 1 auto;
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    max-width: 820px;
    margin: 0 auto;
    width: 100%;
    padding: clamp(0.75rem, 2vw, 1.25rem) clamp(0.75rem, 2vw, 1.5rem) 0;
  }

  .thread-view-chat--thinking-first-turn {
    justify-content: flex-start;
  }

  .messages-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 2rem 0 1.5rem;
    scroll-padding-top: calc(var(--v-layout-top, 64px) + 24px);
  }

  .messages-scroll--thinking-first-turn {
    padding-top: 1rem;
  }

  .messages-scroll--waiting-response {
    padding-bottom: 2rem;
  }

  .input-sticky {
    position: relative;
    flex-shrink: 0;
    padding: 1rem 0 1rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    background: linear-gradient(to top, rgb(var(--v-theme-surface)) 0%, rgba(var(--v-theme-surface), 0.98) 100%);
    backdrop-filter: blur(8px);
  }

  .message-row {
    display: grid;
    grid-template-columns: 1fr;
    width: 100%;

    scroll-margin-top: calc(var(--v-layout-top, 64px) + 24px);
  }

  .message-row-user {
    justify-items: end;
  }

  .message-row-agent {
    justify-items: start;
  }

  .task-status-stack {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    width: 100%;
  }

  .empty-state {
    min-height: 280px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-top: 3rem;
  }

  .waiting-scroll-spacer {
    height: 3rem;
    flex-shrink: 0;
  }

  .thread-actions-menu-list {
    padding: 0.25rem;
    background: transparent;
    width: max-content;
  }

  .thread-actions-menu-item {
    min-height: 36px;
  }

  .thread-app-title {
    color: rgba(var(--v-theme-on-surface), 0.92);
    font-weight: 500;
    letter-spacing: -0.01em;
  }

  @media (max-width: 960px) {
    .thread-view-chat {
      padding: 0.5rem 0 0;
    }

    .messages-scroll {
      padding-bottom: 0.75rem;
    }

    .input-sticky {
      position: sticky;
      bottom: 0;
      padding: 0.5rem 0 calc(env(safe-area-inset-bottom, 0px) + 0.5rem);
      background: linear-gradient(to top, rgb(var(--v-theme-surface)) 18%, rgba(var(--v-theme-surface), 0.94) 100%);
    }

    .input-sticky :deep(.thread-composer-card) {
      max-width: none !important;
      border-radius: 20px 20px 0 0;
    }
  }
</style>

<style lang="scss">
  .thread-actions-menu-content {
    border-radius: 16px;
    border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
    background: rgb(var(--v-theme-surface));
    box-shadow: 0 10px 24px rgba(0, 0, 0, 0.2);
    overflow: hidden;
  }
</style>
