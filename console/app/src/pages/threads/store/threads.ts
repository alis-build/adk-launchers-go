/**
 * Thread list store for the sidebar and threads index page.
 *
 * Manages the reactive list of AG-UI threads fetched from the agui/history
 * ThreadService. Each item merges Thread fields (name, displayName, etc.)
 * with ThreadView fields (readRunCount, hasUnread, pinned) to support both
 * display and read-state tracking.
 *
 * @module pages/threads/store/threads
 */
import { useSelectedAgentStore } from '@/store/selectedAgent'
import { ref, type Ref } from 'vue'
import { fetchThreads } from '../clients'

/**
 * Thread data shape matching the agui/history proto Thread + ThreadView.
 */
export interface ThreadListItem {
  name: string
  displayName: string
  agentId: string
  agentDisplayName: string
  runCount: number
  lastActivityTime?: { seconds?: number | bigint; nanos?: number }
  createTime?: { seconds?: number | bigint; nanos?: number }
  readRunCount: number
  hasUnread: boolean
  pinned: boolean
  pinnedTime?: { seconds?: number | bigint; nanos?: number }
}

/** @deprecated Alias for {@link ThreadListItem}. */
export type ThreadAsObject = ThreadListItem

const threads: Ref<ThreadListItem[]> = ref([])
const loadingLoadThreads: Ref<boolean> = ref(false)
const errorLoadThreads: Ref<string | undefined> = ref(undefined)
const loadingThreadTitles: Ref<Set<string>> = ref(new Set())

/** Snapshot of the current thread list (sidebar / search). */
export function getThreads(): ThreadListItem[] {
  return threads.value
}

/**
 * Normalizes a raw thread object (from REST or gRPC response) into a
 * ThreadListItem. Handles the ThreadView wrapper shape from the list API.
 */
function buildThreadListItem(value: Record<string, unknown>, existing?: ThreadListItem): ThreadListItem | undefined {
  // ThreadView shape from list API: { thread: {...}, readRunCount, hasUnread, pinned, pinnedTime }
  if (value.thread && typeof value.thread === 'object') {
    const thread = value.thread as Record<string, unknown>
    return {
      name: String(thread.name ?? ''),
      displayName: String(thread.displayName ?? thread.display_name ?? ''),
      agentId: String(thread.agentId ?? thread.agent_id ?? ''),
      agentDisplayName: String(thread.agentDisplayName ?? thread.agent_display_name ?? ''),
      runCount: Number(thread.runCount ?? thread.run_count ?? 0),
      lastActivityTime: (thread.lastActivityTime ?? thread.last_activity_time) as ThreadListItem['lastActivityTime'],
      createTime: (thread.createTime ?? thread.create_time) as ThreadListItem['createTime'],
      readRunCount: Number(value.readRunCount ?? value.read_run_count ?? 0),
      hasUnread: Boolean(value.hasUnread ?? value.has_unread ?? false),
      pinned: Boolean(value.pinned ?? false),
      pinnedTime: (value.pinnedTime ?? value.pinned_time) as ThreadListItem['pinnedTime'],
    }
  }

  // Flat Thread shape (from getThread or optimistic creation)
  return {
    name: String(value.name ?? ''),
    displayName: String(value.displayName ?? value.display_name ?? ''),
    agentId: String(value.agentId ?? value.agent_id ?? ''),
    agentDisplayName: String(value.agentDisplayName ?? value.agent_display_name ?? ''),
    runCount: Number(value.runCount ?? value.run_count ?? existing?.runCount ?? 0),
    lastActivityTime: (value.lastActivityTime ?? value.last_activity_time ?? existing?.lastActivityTime) as ThreadListItem['lastActivityTime'],
    createTime: (value.createTime ?? value.create_time ?? existing?.createTime) as ThreadListItem['createTime'],
    readRunCount: Number(value.readRunCount ?? value.read_run_count ?? existing?.readRunCount ?? 0),
    hasUnread: Boolean(value.hasUnread ?? value.has_unread ?? existing?.hasUnread ?? false),
    pinned: Boolean(value.pinned ?? existing?.pinned ?? false),
    pinnedTime: (value.pinnedTime ?? value.pinned_time ?? existing?.pinnedTime) as ThreadListItem['pinnedTime'],
  }
}

/** True when the thread has runs newer than {@link ThreadListItem.readRunCount}. */
export function getThreadHasUnread(thread: ThreadListItem | undefined): boolean {
  if (!thread) return false
  return thread.hasUnread === true
}

/** Unused with AG-UI history; always returns undefined (scroll-to-bottom UX). */
export function getThreadResumeSequence(_thread: ThreadListItem | undefined): bigint | undefined {
  // agui/history uses run_count instead of event sequences.
  // No resume-to-sequence scrolling; always scroll to bottom.
  return undefined
}

/** Replaces the in-memory list from raw API objects. */
export function setThreads(value: Array<Record<string, unknown>>): void {
  threads.value = value.map((thread) => buildThreadListItem(thread)).filter((thread): thread is ThreadListItem => thread !== undefined)
}

/** Prepends or replaces a thread by name (e.g. optimistic new thread). */
export function addThread(t: Record<string, unknown> | ThreadListItem): void {
  const next = buildThreadListItem(t as Record<string, unknown>)
  if (!next) return
  threads.value = [next, ...threads.value.filter((thread) => thread.name !== next.name)]
}

/** Merges one thread (flat or ThreadView shape) into the list by name. */
export function updateThread(t: Record<string, unknown> | ThreadListItem): void {
  const raw = t as Record<string, unknown>
  const threadName = raw.thread ? String((raw.thread as Record<string, unknown>).name ?? '') : String(raw.name ?? '')
  if (!threadName) return

  const existing = threads.value.find((thread) => thread.name === threadName)
  const next = buildThreadListItem(raw, existing)
  if (!next) return

  threads.value = threads.value.map((thread) => (thread.name === next.name ? next : thread))
}

/** Removes a thread from the sidebar list by resource name. */
export function removeThread(threadName: string): void {
  threads.value = threads.value.filter((thread) => thread.name !== threadName)
}

/** Tracks threads awaiting display title from the server (optimistic new threads). */
export function setThreadTitleLoading(threadName: string, loading: boolean): void {
  const next = new Set(loadingThreadTitles.value)
  if (loading) {
    next.add(threadName)
  } else {
    next.delete(threadName)
  }
  loadingThreadTitles.value = next
}

/** Returns whether the thread title is still being generated. */
export function isThreadTitleLoading(threadName: string): boolean {
  return loadingThreadTitles.value.has(threadName)
}

/** Reactive loading flag for {@link loadThreads}. */
export function getLoadingLoadThreads(): Ref<boolean> {
  return loadingLoadThreads
}

/** Last error from {@link loadThreads}, if any. */
export function getErrorLoadThreads(): Ref<string | undefined> {
  return errorLoadThreads
}

/**
 * Fetches the full thread list from the agui/history REST endpoint.
 */
export async function loadThreads(): Promise<void> {
  loadingLoadThreads.value = true
  errorLoadThreads.value = undefined
  try {
    const agentStore = useSelectedAgentStore()
    const response = await fetchThreads({ agentId: agentStore.selectedAgentId || undefined })
    const list = (response.threads ?? []).map((tv) => buildThreadListItem(tv as Record<string, unknown>)).filter((thread): thread is ThreadListItem => thread !== undefined)
    if (list.length > 0) {
      threads.value = list
      const next = new Set(loadingThreadTitles.value)
      for (const thread of list) {
        if (thread.displayName?.trim()) {
          next.delete(thread.name)
        }
      }
      loadingThreadTitles.value = next
    } else {
      threads.value = []
    }
  } catch (e) {
    errorLoadThreads.value = (e as Error).message ?? 'Failed to load threads'
    threads.value = []
  } finally {
    loadingLoadThreads.value = false
  }
}
