<script setup lang="ts">
  import JsonTreeNode from '@/pages/threads/components/JsonTreeNode.vue'
  import { getChatMessageDebugJson } from '@/pages/threads/store/aguiDebug'
  import { useThreadViewDialogStore } from '@/pages/threads/store/threadViewDialog'
  import { getPayloadTypeLabel } from '@/pages/threads/types'
  import { useSnackbarStore } from '@/store/snackbar'
  import { storeToRefs } from 'pinia'
  import { computed } from 'vue'

  const store = useThreadViewDialogStore()
  const { debugDialogMessage } = storeToRefs(store)
  const snackbarStore = useSnackbarStore()

  const msg = computed(() => debugDialogMessage.value)

  function sanitizeJsonValue(value: unknown, seen = new WeakSet<object>()): unknown {
    // Values that JSON.stringify cannot represent safely in the debug panel.
    if (typeof value === 'bigint') {
      return value.toString()
    }
    if (value instanceof Uint8Array) {
      return Array.from(value)
    }
    if (typeof value === 'function') {
      return `[Function ${value.name || 'anonymous'}]`
    }
    if (Array.isArray(value)) {
      return value.map((item) => sanitizeJsonValue(item, seen))
    }
    if (value && typeof value === 'object') {
      if (seen.has(value)) {
        return '[Circular]'
      }
      seen.add(value)
      const out: Record<string, unknown> = {}
      for (const [key, entry] of Object.entries(value)) {
        out[key] = sanitizeJsonValue(entry, seen)
      }
      return out
    }
    return value
  }

  const debugValue = computed(() => {
    if (!msg.value) return {}
    try {
      return sanitizeJsonValue(getChatMessageDebugJson(msg.value))
    } catch {
      return { error: 'Unable to serialize message JSON' }
    }
  })

  const debugJson = computed(() => JSON.stringify(debugValue.value, null, 2))

  function close() {
    store.closeDebugDialog()
  }

  function onDialogModel(open: boolean | null) {
    if (open === false) {
      store.closeDebugDialog()
    }
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
    :model-value="store.debugDialogOpen"
    max-width="900"
    @update:model-value="onDialogModel"
  >
    <v-card v-if="msg">
      <v-card-title class="d-flex align-center justify-space-between text-headline-small py-4">
        Message Debug JSON
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

      <v-card-text class="pa-4">
        <div class="d-flex justify-space-between align-center mb-3">
          <div class="text-body-small text-medium-emphasis">
            {{ getPayloadTypeLabel(msg.payloadType) }} · {{ msg.resourceName || msg.id }}
            <template v-if="msg.debugData?.kind">
              · {{ msg.debugData.kind }}
            </template>
          </div>
          <v-btn
            size="small"
            variant="text"
            prepend-icon="content_copy"
            @click="copyToClipboard(debugJson)"
          >
            Copy JSON
          </v-btn>
        </div>

        <div class="debug-json">
          <JsonTreeNode :value="debugValue" />
        </div>
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
  .debug-json {
    padding: 12px;
    max-height: min(65vh, 680px);
    overflow: auto;
    border-radius: 12px;
    background: rgba(0, 0, 0, 0.05);
  }
</style>
