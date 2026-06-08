<script setup lang="ts">
  /** Single row in the batch checklist — Run/Skip toggle with optional arg editing. */
  import {
    formatArgsInline,
    getInterruptRiskTier,
    getInterruptTitle,
    riskTierLabel,
  } from '@/components/chat/toolInterruptDisplay'
  import { useToolArgEdit } from '@/components/chat/useToolArgEdit'
  import type { ChatToolCall, Interrupt } from '@/pages/threads/types'
  import { computed, ref, toRef } from 'vue'

  const props = defineProps<{
    interrupt: Interrupt
    toolCall?: ChatToolCall
    /** true = Run (approved), false = Skip */
    approved: boolean
    disabled?: boolean
  }>()

  const emit = defineEmits<{
    (e: 'update:approved', value: boolean): void
    (e: 'update:edited-args', args: Record<string, unknown> | undefined): void
  }>()

  const showDetails = ref(false)
  const showEditMode = ref(false)

  const title = computed(() => getInterruptTitle(props.interrupt, props.toolCall))
  const inlineArgs = computed(() => formatArgsInline(props.toolCall?.args))
  const riskTier = computed(() => getInterruptRiskTier(props.interrupt))

  const canEdit = computed(() => {
    const schema = props.interrupt.responseSchema as Record<string, unknown> | undefined
    const propsSchema = schema?.properties as Record<string, unknown> | undefined
    return propsSchema?.editedArgs !== undefined
  })

  const toolCallRef = toRef(props, 'toolCall')
  const {
    editedArgsJson,
    jsonError,
    isValidJson,
    validateJson,
    buildPayload,
  } = useToolArgEdit(toolCallRef)

  function setApproved(approved: boolean) {
    emit('update:approved', approved)
  }

  function saveEdits() {
    const payload = buildPayload(true, true)
    if (payload?.editedArgs) {
      emit('update:edited-args', payload.editedArgs)
      showEditMode.value = false
    }
  }
</script>

<template>
  <div
    class="tool-batch-row pa-3"
    data-test="tool-batch-row"
  >
    <div class="d-flex align-start">
      <v-icon
        :icon="approved ? 'check_circle' : 'cancel'"
        :color="approved ? 'success' : 'error'"
        class="mr-2 mt-1"
        size="small"
      />
      <div class="flex-grow-1">
        <div class="d-flex align-center flex-wrap gap-2 mb-1">
          <span class="font-weight-medium">{{ toolCall?.name ?? title }}</span>
          <v-chip
            v-if="riskTier"
            size="x-small"
            variant="tonal"
          >
            {{ riskTierLabel(riskTier) }}
          </v-chip>
        </div>
        <div
          v-if="inlineArgs"
          class="text-body-small text-medium-emphasis"
        >
          {{ inlineArgs }}
          <v-btn
            variant="text"
            size="x-small"
            @click="showDetails = !showDetails"
          >
            details
          </v-btn>
          <v-btn
            v-if="canEdit"
            variant="text"
            size="x-small"
            @click="showEditMode = !showEditMode"
          >
            edit
          </v-btn>
        </div>
        <pre
          v-if="showDetails && toolCall?.args"
          class="tool-args-full text-body-small mt-2"
        >{{ JSON.stringify(toolCall.args, null, 2) }}</pre>
        <div
          v-if="showEditMode"
          class="mt-2"
        >
          <v-textarea
            v-model="editedArgsJson"
            label="Edited arguments (JSON)"
            variant="outlined"
            density="compact"
            rows="4"
            :error-messages="jsonError"
            :disabled="disabled"
            @input="validateJson"
          />
          <v-btn
            size="small"
            color="primary"
            class="mr-2"
            :disabled="!isValidJson || disabled"
            @click="saveEdits"
          >
            Save args
          </v-btn>
          <v-btn
            size="small"
            variant="text"
            @click="showEditMode = false"
          >
            Cancel
          </v-btn>
        </div>
      </div>
      <v-btn-toggle
        :model-value="approved"
        mandatory
        density="compact"
        class="ml-2"
        :disabled="disabled"
        @update:model-value="setApproved(!!$event)"
      >
        <v-btn
          :value="true"
          size="small"
          color="success"
        >
          Run
        </v-btn>
        <v-btn
          :value="false"
          size="small"
        >
          Skip
        </v-btn>
      </v-btn-toggle>
    </div>
  </div>
</template>

<style lang="scss" scoped>
  .tool-batch-row {
    border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.08);

    &:last-child {
      border-bottom: none;
    }
  }

  .tool-args-full {
    background: rgba(var(--v-theme-on-surface), 0.05);
    padding: 8px;
    border-radius: 4px;
    overflow-x: auto;
    margin: 0;
  }
</style>
