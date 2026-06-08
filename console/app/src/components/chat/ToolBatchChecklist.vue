<script setup lang="ts">
  /** Multi-interrupt checklist — approve/skip each action, then submit all at once. */
  import ExpirationTimer from '@/components/chat/ExpirationTimer.vue'
  import ToolBatchRow from '@/components/chat/ToolBatchRow.vue'
  import { batchFooterLabel } from '@/components/chat/toolInterruptDisplay'
  import { buildInterruptResumeEntries, type InterruptUserInputState } from '@/components/chat/toolInterruptResume'
  import type { ChatToolCall, Interrupt, ResumeEntry, ToolConfirmationPayload } from '@/pages/threads/types'
  import { computed, reactive, ref, watch } from 'vue'

  const props = defineProps<{
    interrupts: Interrupt[]
    toolCalls: ChatToolCall[]
    disabled?: boolean
  }>()

  const emit = defineEmits<{
    (e: 'submit-resume', resumeEntries: ResumeEntry[]): void
    (e: 'expired'): void
  }>()

  /** Per interrupt: decided?, approved?, optional editedArgs */
  const rowState = reactive<Record<string, {
    decided: boolean
    approved: boolean
    editedArgs?: Record<string, unknown>
  }>>({})

  const hasExpiredInterrupts = ref(false)

  watch(
    () => props.interrupts,
    (interrupts) => {
      for (const intr of interrupts) {
        if (!rowState[intr.id]) {
          rowState[intr.id] = { decided: false, approved: true }
        }
      }
    },
    { immediate: true },
  )

  const allDecided = computed(() =>
    props.interrupts.length > 0
    && props.interrupts.every((intr) => rowState[intr.id]?.decided),
  )

  const runCount = computed(() =>
    props.interrupts.filter((intr) => rowState[intr.id]?.decided && rowState[intr.id]?.approved).length,
  )

  const skipCount = computed(() =>
    props.interrupts.filter((intr) => rowState[intr.id]?.decided && !rowState[intr.id]?.approved).length,
  )

  const footerLabel = computed(() => batchFooterLabel(runCount.value, props.interrupts))

  const earliestExpiry = computed(() => {
    const dates = props.interrupts
      .map((i) => i.expiresAt)
      .filter((d): d is string => !!d)
    if (!dates.length) return undefined
    return dates.sort()[0]
  })

  function findToolCall(toolCallId?: string): ChatToolCall | undefined {
    if (!toolCallId) return undefined
    return props.toolCalls.find((c) => c.id === toolCallId)
  }

  function setApproved(interruptId: string, approved: boolean) {
    const row = rowState[interruptId]
    if (!row) return
    row.approved = approved
    row.decided = true
    if (!approved) row.editedArgs = undefined
  }

  function setEditedArgs(interruptId: string, args: Record<string, unknown> | undefined) {
    const row = rowState[interruptId]
    if (!row) return
    row.editedArgs = args
    row.decided = true
    row.approved = true
  }

  function approveAll() {
    for (const intr of props.interrupts) {
      rowState[intr.id] = { decided: true, approved: true, editedArgs: rowState[intr.id]?.editedArgs }
    }
  }

  function rejectAll() {
    for (const intr of props.interrupts) {
      rowState[intr.id] = { decided: true, approved: false }
    }
    submitAll()
  }

  function buildUserInputs(): Record<string, InterruptUserInputState> {
    const inputs: Record<string, InterruptUserInputState> = {}
    for (const intr of props.interrupts) {
      const row = rowState[intr.id]
      if (!row?.decided) continue
      const payload: ToolConfirmationPayload = { approved: row.approved }
      if (row.approved && row.editedArgs) {
        payload.editedArgs = row.editedArgs
      }
      inputs[intr.id] = { payload, cancelled: false }
    }
    return inputs
  }

  function submitAll() {
    if (!allDecided.value) return
    emit('submit-resume', buildInterruptResumeEntries(props.interrupts, buildUserInputs()))
  }

  defineExpose({ submitAll })
</script>

<template>
  <v-card
    variant="outlined"
    class="tool-batch-checklist"
    data-test="tool-batch-checklist"
  >
    <v-card-title class="d-flex align-center text-body-large py-3">
      <span>Review {{ interrupts.length }} actions</span>
      <v-spacer />
      <ExpirationTimer
        v-if="earliestExpiry"
        :expires-at="earliestExpiry"
        class="mr-2"
        @expired="hasExpiredInterrupts = true; emit('expired')"
      />
      <v-btn
        variant="text"
        size="small"
        data-test="approve-all"
        :disabled="disabled || hasExpiredInterrupts"
        @click="approveAll"
      >
        Approve all
      </v-btn>
    </v-card-title>
    <v-card-subtitle class="pb-0">
      Toggle each, or approve all — then run.
    </v-card-subtitle>

    <ToolBatchRow
      v-for="intr in interrupts"
      :key="intr.id"
      :interrupt="intr"
      :tool-call="findToolCall(intr.toolCallId)"
      :approved="rowState[intr.id]?.approved ?? true"
      :disabled="disabled || hasExpiredInterrupts"
      @update:approved="setApproved(intr.id, $event)"
      @update:edited-args="setEditedArgs(intr.id, $event)"
    />

    <v-card-actions class="px-4 py-3">
      <span class="text-body-small text-medium-emphasis">
        <template v-if="allDecided">
          {{ runCount }} to run<span v-if="skipCount"> · {{ skipCount }} skipped</span>
        </template>
        <template v-else>
          Decide each action above
        </template>
      </span>
      <v-spacer />
      <v-btn
        variant="outlined"
        color="error"
        size="small"
        :disabled="disabled"
        @click="rejectAll"
      >
        Reject all
      </v-btn>
      <v-btn
        color="primary"
        size="small"
        class="ml-2"
        data-test="batch-submit"
        :disabled="!allDecided || hasExpiredInterrupts || disabled"
        @click="submitAll"
      >
        {{ footerLabel }}
      </v-btn>
    </v-card-actions>
  </v-card>
</template>
