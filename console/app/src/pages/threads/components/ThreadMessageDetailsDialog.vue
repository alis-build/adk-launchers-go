<script setup lang="ts">
  import { useThreadViewDialogStore } from '@/pages/threads/store/threadViewDialog'
  import { getPayloadTypeLabel, getStateText, isFinalStatus, getRoleLabel } from '@/pages/threads/types'
  import { getRuntimeConfig } from '@/runtimeConfig'
  import { useSnackbarStore } from '@/store/snackbar'
  import { storeToRefs } from 'pinia'
  import { computed, unref, type Ref } from 'vue'

  const store = useThreadViewDialogStore()
  const { detailsDialogMessage } = storeToRefs(store)
  const snackbarStore = useSnackbarStore()

  const msg = computed(() => detailsDialogMessage.value)

  const artifactEnvelope = computed((): Record<string, unknown> | undefined => {
    if (msg.value?.artifactEnvelopeMetadata) return msg.value.artifactEnvelopeMetadata
    const raw = unref(store.detailsDialogArtifactEnvelopeMetadata as Ref<Record<string, unknown> | undefined> | Record<string, unknown> | undefined)
    if (raw === undefined || raw === null) return undefined
    if (typeof raw === 'object' && !Array.isArray(raw)) return raw as Record<string, unknown>
    return undefined
  })

  const adkUsage = computed((): Record<string, unknown> | null => {
    const env = artifactEnvelope.value
    const u = env?.adk_usage_metadata
    if (!u || typeof u !== 'object' || Array.isArray(u)) return null
    return u as Record<string, unknown>
  })

  const adkInvocationId = computed((): string | undefined => {
    const v = artifactEnvelope.value?.adk_invocation_id
    return typeof v === 'string' && v ? v : undefined
  })
  const traceProjectId = (getRuntimeConfig().gcpProject ?? '').trim()

  const traceHref = computed((): string | undefined => {
    if (!adkInvocationId.value || !traceProjectId) return undefined

    // Cloud Trace explorer heatmap filtered by Vertex agent invocation id (same shape as ThreadView).
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
              value: [adkInvocationId.value],
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
  })

  const adkTokenCount = computed((): number | undefined => {
    const u = adkUsage.value
    if (!u) return undefined
    if (typeof u.tokenCount === 'number') return u.tokenCount
    if (typeof u.candidatesTokenCount === 'number') return u.candidatesTokenCount
    return undefined
  })

  const adkThoughtsTokenCount = computed((): number | undefined => {
    const v = adkUsage.value?.thoughtsTokenCount
    return typeof v === 'number' ? v : undefined
  })

  const adkTotalTokenCount = computed((): number | undefined => {
    const v = adkUsage.value?.totalTokenCount
    return typeof v === 'number' ? v : undefined
  })

  const hasAdkDetails = computed(() => adkInvocationId.value !== undefined || adkTokenCount.value !== undefined || adkThoughtsTokenCount.value !== undefined || adkTotalTokenCount.value !== undefined)

  function close() {
    store.resetDialogState()
  }

  function onDialogModel(open: boolean | null) {
    if (open === false) {
      store.resetDialogState()
    }
  }

  const formatTimestamp = (timestamp: number): string => {
    return new Date(timestamp).toLocaleString()
  }

  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      snackbarStore.success('Copied to clipboard')
    } catch (error) {
      console.error('Failed to copy:', error)
      snackbarStore.error('Failed to copy to clipboard')
    }
  }
</script>

<template>
  <v-dialog
    :model-value="store.detailsDialogOpen"
    max-width="520"
    @update:model-value="onDialogModel"
  >
    <v-card v-if="msg">
      <v-card-title class="d-flex align-center justify-space-between text-headline-small py-4">
        Message Details
        <v-btn
          icon
          variant="text"
          density="comfortable"
          aria-label="Close"
          @click="close"
        >
          <v-icon>close</v-icon>
        </v-btn>
      </v-card-title>

      <v-divider />

      <v-card-text class="pa-0">
        <v-list
          lines="two"
          density="compact"
          class="bg-transparent message-details-list pa-4"
        >
          <v-list-item class="px-0">
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                category
              </v-icon>
            </template>
            <v-list-item-title>Type</v-list-item-title>
            <v-list-item-subtitle>{{ getPayloadTypeLabel(msg.payloadType) }}</v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="msg.resourceName"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                bookmark
              </v-icon>
            </template>
            <v-list-item-title class="d-flex align-center">
              Event
              <v-btn
                icon
                size="x-small"
                variant="text"
                class="ml-1"
                @click="copyToClipboard(msg.resourceName!)"
              >
                <v-icon size="14">content_copy</v-icon>
              </v-btn>
            </v-list-item-title>
            <v-list-item-subtitle class="text-truncate">
              {{ msg.resourceName }}
            </v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="msg.id"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                fingerprint
              </v-icon>
            </template>
            <v-list-item-title class="d-flex align-center">
              Message ID
              <v-btn
                icon
                size="x-small"
                variant="text"
                class="ml-1"
                @click="copyToClipboard(msg.id)"
              >
                <v-icon size="14">content_copy</v-icon>
              </v-btn>
            </v-list-item-title>
            <v-list-item-subtitle class="text-truncate">
              {{ msg.id }}
            </v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="msg.contextId"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                account_tree
              </v-icon>
            </template>
            <v-list-item-title class="d-flex align-center">
              Context ID
              <v-btn
                icon
                size="x-small"
                variant="text"
                class="ml-1"
                @click="copyToClipboard(msg.contextId!)"
              >
                <v-icon size="14">content_copy</v-icon>
              </v-btn>
            </v-list-item-title>
            <v-list-item-subtitle class="text-truncate">
              {{ msg.contextId }}
            </v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="msg.taskId"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                task
              </v-icon>
            </template>
            <v-list-item-title class="d-flex align-center">
              Task ID
              <v-btn
                icon
                size="x-small"
                variant="text"
                class="ml-1"
                @click="copyToClipboard(msg.taskId!)"
              >
                <v-icon size="14">content_copy</v-icon>
              </v-btn>
            </v-list-item-title>
            <v-list-item-subtitle class="text-truncate">
              {{ msg.taskId }}
            </v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="getRoleLabel(msg)"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                person
              </v-icon>
            </template>
            <v-list-item-title>Role</v-list-item-title>
            <v-list-item-subtitle>{{ getRoleLabel(msg) }}</v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="msg.timestamp !== undefined"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                schedule
              </v-icon>
            </template>
            <v-list-item-title>Timestamp</v-list-item-title>
            <v-list-item-subtitle>{{ formatTimestamp(msg.timestamp) }}</v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="getStateText(msg.status)"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                sync_alt
              </v-icon>
            </template>
            <v-list-item-title>State</v-list-item-title>
            <v-list-item-subtitle>{{ getStateText(msg.status) }}</v-list-item-subtitle>
          </v-list-item>

          <v-list-item
            v-if="msg.payloadType === 'status_update'"
            class="px-0"
          >
            <template #prepend>
              <v-icon
                color="grey"
                class="mr-2"
              >
                flag
              </v-icon>
            </template>
            <v-list-item-title>Final</v-list-item-title>
            <v-list-item-subtitle>{{ isFinalStatus(msg.status) ? 'Yes' : 'No' }}</v-list-item-subtitle>
          </v-list-item>

          <template v-if="hasAdkDetails">
            <v-list-item
              v-if="adkInvocationId"
              class="px-0"
            >
              <template #prepend>
                <v-icon
                  color="grey"
                  class="mr-2"
                >
                  bolt
                </v-icon>
              </template>
              <v-list-item-title class="d-flex align-center">
                Invocation ID
                <v-btn
                  icon
                  size="x-small"
                  variant="text"
                  class="ml-1"
                  @click="copyToClipboard(adkInvocationId)"
                >
                  <v-icon size="14">content_copy</v-icon>
                </v-btn>
                <v-btn
                  v-if="traceHref"
                  icon
                  size="x-small"
                  variant="text"
                  class="ml-1"
                  :href="traceHref"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <v-icon size="14">troubleshoot</v-icon>
                </v-btn>
              </v-list-item-title>
              <v-list-item-subtitle class="text-truncate">
                {{ adkInvocationId }}
              </v-list-item-subtitle>
            </v-list-item>

            <v-list-item
              v-if="adkTokenCount !== undefined"
              class="px-0"
            >
              <template #prepend>
                <v-icon
                  color="grey"
                  class="mr-2"
                >
                  text_fields
                </v-icon>
              </template>
              <v-list-item-title>Token count</v-list-item-title>
              <v-list-item-subtitle>{{ adkTokenCount }}</v-list-item-subtitle>
            </v-list-item>

            <v-list-item
              v-if="adkThoughtsTokenCount !== undefined"
              class="px-0"
            >
              <template #prepend>
                <v-icon
                  color="grey"
                  class="mr-2"
                >
                  psychology
                </v-icon>
              </template>
              <v-list-item-title>Thoughts token count</v-list-item-title>
              <v-list-item-subtitle>{{ adkThoughtsTokenCount }}</v-list-item-subtitle>
            </v-list-item>

            <v-list-item
              v-if="adkTotalTokenCount !== undefined"
              class="px-0"
            >
              <template #prepend>
                <v-icon
                  color="grey"
                  class="mr-2"
                >
                  data_usage
                </v-icon>
              </template>
              <v-list-item-title>Total token count</v-list-item-title>
              <v-list-item-subtitle>{{ adkTotalTokenCount }}</v-list-item-subtitle>
            </v-list-item>
          </template>
        </v-list>
      </v-card-text>

      <v-divider />

      <v-card-actions class="pa-3">
        <v-spacer />
        <v-btn
          variant="text"
          @click="close"
        >
          Close
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style lang="scss" scoped>
  .text-truncate {
    max-width: min(100%, 420px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .message-details-list {
    max-height: min(60vh, 480px);
    overflow-y: auto;

    :deep(.v-list-item__content) {
      text-align: left;
    }

    :deep(.v-list-item-title),
    :deep(.v-list-item-subtitle) {
      text-align: left;
    }
  }
</style>
