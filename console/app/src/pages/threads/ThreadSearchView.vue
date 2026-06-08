<script setup lang="ts">
  import { computed, nextTick, onMounted, ref, watch } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { useSelectedAgentStore } from '@/store/selectedAgent'
  import { getThreads, loadThreads, type ThreadListItem } from './store/threads'
  import { getContextIdFromThreadName } from './utils'

  type TimestampLike = {
    seconds?: number | bigint
    nanos?: number
  }

  type SearchThreadItem = {
    thread: ThreadListItem
    title: string
    contextId: string
    subtitle: string
    recencyLabel: string
    timestampMs: number
  }

  const route = useRoute()
  const router = useRouter()
  const agentStore = useSelectedAgentStore()
  const searchInput = ref<{ focus?: () => void } | null>(null)
  const searchText = ref(typeof route.query.q === 'string' ? route.query.q : '')
  const loading = ref(false)

  const sortedThreads = computed(() => [...getThreads()].sort((left, right) => timestampMs(right.createTime) - timestampMs(left.createTime)))

  const threadItems = computed<SearchThreadItem[]>(() =>
    sortedThreads.value.map((thread) => {
      const contextId = getContextIdFromThreadName(thread.name)
      const title = thread.displayName?.trim() || `Thread ${contextId.slice(0, 8) || 'Untitled'}`
      const timestamp = timestampMs(thread.createTime)

      return {
        thread,
        title,
        contextId,
        subtitle: contextId,
        recencyLabel: formatRecencyLabel(timestamp),
        timestampMs: timestamp,
      }
    }),
  )

  const normalizedSearch = computed(() => searchText.value.trim().toLowerCase())

  const filteredItems = computed(() => {
    if (!normalizedSearch.value) {
      return threadItems.value.slice(0, 12)
    }

    return threadItems.value.filter((item) => {
      const haystack = `${item.title} ${item.subtitle}`.toLowerCase()
      return haystack.includes(normalizedSearch.value)
    })
  })

  const searchResultsLabel = computed(() => {
    if (!normalizedSearch.value) {
      return 'Recent'
    }
    return filteredItems.value.length === 1 ? '1 result' : `${filteredItems.value.length} results`
  })

  onMounted(async () => {
    if (!getThreads().length) {
      loading.value = true
      await loadThreads()
      loading.value = false
    }

    await nextTick()
    searchInput.value?.focus?.()
  })

  watch(
    () => route.query.q,
    (value) => {
      const nextValue = typeof value === 'string' ? value : ''
      if (nextValue !== searchText.value) {
        searchText.value = nextValue
      }
    },
  )

  watch(searchText, (value) => {
    const nextValue = value.trim()
    const currentValue = typeof route.query.q === 'string' ? route.query.q : ''
    if (nextValue === currentValue) return

    void router.replace({
      name: 'ThreadSearch',
      query: nextValue ? { q: nextValue } : {},
    })
  })

  function timestampMs(timestamp?: TimestampLike): number {
    if (!timestamp) return 0
    return Number(timestamp.seconds ?? 0) * 1000 + Math.floor((timestamp.nanos ?? 0) / 1_000_000)
  }

  function onSearchResultClick(item: SearchThreadItem): void {
    if (item.thread.agentId) {
      agentStore.setSelectedAgentId(item.thread.agentId)
    }
  }

  function formatRecencyLabel(timestamp: number): string {
    if (!timestamp) return 'Unknown'

    const now = new Date()
    const date = new Date(timestamp)

    if (Number.isNaN(date.getTime())) return 'Unknown'

    const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    const yesterdayStart = todayStart - 24 * 60 * 60 * 1000

    if (timestamp >= todayStart) return 'Today'
    if (timestamp >= yesterdayStart) return 'Yesterday'

    return new Intl.DateTimeFormat(undefined, {
      day: 'numeric',
      month: 'short',
    }).format(date)
  }
</script>

<template>
  <v-container class="thread-search-page fill-height py-4 py-md-8">
    <v-row justify="center">
      <v-col
        cols="12"
        md="11"
        lg="10"
        xl="9"
      >
        <v-card
          rounded="xl"
          variant="flat"
          class="search-panel"
        >
          <div class="px-0 pt-0 pb-4 pb-md-5">
            <v-text-field
              ref="searchInput"
              v-model="searchText"
              variant="outlined"
              density="comfortable"
              hide-details
              rounded="pill"
              clearable
              persistent-clear
              placeholder="Search for threads"
              class="search-field"
            >
              <template #prepend-inner>
                <v-icon size="20">search</v-icon>
              </template>
            </v-text-field>
          </div>

          <div class="px-0 pb-0">
            <div class="search-results-header d-flex align-center justify-space-between mb-4">
              <div class="text-title-medium">{{ searchResultsLabel }}</div>
              <div class="text-body-small text-medium-emphasis">
                {{ normalizedSearch ? 'Matching titles and IDs' : 'Most recent threads' }}
              </div>
            </div>

            <v-skeleton-loader
              v-if="loading"
              type="list-item-two-line@6"
              color="transparent"
            />

            <v-list
              v-else-if="filteredItems.length"
              variant="text"
              bg-color="transparent"
              density="compact"
              class="results-list"
            >
              <v-list-item
                v-for="item in filteredItems"
                :key="item.thread.name"
                rounded="xl"
                class="result-item"
                :to="{ name: 'Thread', params: { threadId: item.contextId } }"
                @click="onSearchResultClick(item)"
              >
                <v-list-item-title class="result-item__title text-truncate">
                  {{ item.title }}
                </v-list-item-title>

                <template #append>
                  <div class="result-meta text-body-small">
                    {{ item.recencyLabel }}
                  </div>
                </template>
              </v-list-item>
            </v-list>

            <v-card
              v-else
              rounded="xl"
              variant="tonal"
              class="results-empty pa-5"
            >
              <div class="text-body-medium">No threads match that search.</div>
              <div class="text-body-small text-medium-emphasis mt-1">Try a display name or thread ID.</div>
            </v-card>
          </div>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

<style scoped lang="scss">
  .thread-search-page {
    height: 100%;
    min-height: 0;
    align-items: flex-start;
    overflow-y: auto;
    overflow-x: hidden;
    -webkit-overflow-scrolling: touch;
  }

  .thread-search-header {
    color: rgba(var(--v-theme-on-surface), 0.94);
  }

  .thread-search-title {
    line-height: 1.05;
    letter-spacing: -0.04em !important;
    font-weight: 400;
    font-size: clamp(2.25rem, 4vw, 3.25rem) !important;
  }

  .search-panel {
    background: transparent;
    border: 0;
    box-shadow: none;
  }

  .search-results-header {
    padding-inline: 1.5rem;
  }

  .search-field :deep(.v-field__outline) {
    --v-field-border-opacity: 0.12;
  }

  .search-field :deep(.v-field--focused) {
    box-shadow:
      inset 0 0 0 1px rgba(var(--v-theme-primary), 0.32),
      0 8px 18px rgba(0, 0, 0, 0.08);
  }

  .search-field :deep(.v-field__input) {
    min-height: 60px;
    font-size: 1rem;
  }

  .search-field :deep(.v-field) {
    background: rgba(var(--v-theme-on-surface), 0.02);
  }

  .results-list {
    padding: 0;
  }

  .result-item {
    min-height: 3rem;
    padding-inline: 1.25rem;
    border-radius: 1.75rem;
    transition:
      background-color 0.16s ease,
      transform 0.16s ease;
  }

  .result-item:hover {
    background: rgba(var(--v-theme-on-surface), 0.03);
  }

  .result-item--selected,
  .result-item.v-list-item--active {
    background: rgba(var(--v-theme-on-surface), 0.08);
  }

  .result-item :deep(.v-list-item__append) {
    align-self: center;
  }

  .result-item :deep(.v-list-item__content) {
    padding-right: 0.75rem;
  }

  .result-item__title {
    color: rgba(var(--v-theme-on-surface), 0.92);
    font-size: 0.98rem;
    font-weight: 300;
  }

  .result-meta {
    color: rgba(var(--v-theme-on-surface), 0.68);
    min-width: 4rem;
    text-align: right;
    font-size: 0.92rem;
  }

  .results-empty {
    background: rgba(var(--v-theme-on-surface), 0.04);
    margin-inline: 1.5rem;
  }

  @media (max-width: 960px) {
    .thread-search-title {
      font-size: clamp(1.75rem, 7vw, 2.5rem) !important;
    }

    .search-results-header,
    .result-item,
    .results-empty {
      padding-inline: 1rem;
      margin-inline: 0;
    }
  }
</style>
