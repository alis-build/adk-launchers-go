<script setup lang="ts">
  /** Renders a single ToolPermissionCard or a ToolBatchChecklist depending on interrupt count. */
  import ToolBatchChecklist from '@/components/chat/ToolBatchChecklist.vue'
  import ToolPermissionCard from '@/components/chat/ToolPermissionCard.vue'
  import type { ChatToolCall, Interrupt, ResumeEntry } from '@/pages/threads/types'
  import { computed, ref } from 'vue'

  const props = defineProps<{
    interrupts: Interrupt[]
    toolCalls: ChatToolCall[]
    disabled?: boolean
  }>()

  const emit = defineEmits<{
    (e: 'submit-resume', resumeEntries: ResumeEntry[]): void
  }>()

  const submitError = ref<string | undefined>(undefined)
  const hasExpiredInterrupts = ref(false)

  const isSingle = computed(() => props.interrupts.length === 1)

  function onSubmitResume(entries: ResumeEntry[]) {
    submitError.value = undefined
    emit('submit-resume', entries)
  }

  function handleExpiration() {
    hasExpiredInterrupts.value = true
  }

  defineExpose({ submitting: computed(() => props.disabled) })
</script>

<template>
  <div class="tool-interrupt-resolver my-2">
    <ToolPermissionCard
      v-if="isSingle"
      :interrupt="interrupts[0]!"
      :tool-call="toolCalls.find((c) => c.id === interrupts[0]?.toolCallId)"
      :disabled="disabled || hasExpiredInterrupts"
      @submit-resume="onSubmitResume"
      @expired="handleExpiration"
    />

    <ToolBatchChecklist
      v-else
      :interrupts="interrupts"
      :tool-calls="toolCalls"
      :disabled="disabled || hasExpiredInterrupts"
      @submit-resume="onSubmitResume"
      @expired="handleExpiration"
    />

    <v-alert
      v-if="submitError"
      type="error"
      class="mt-3"
      closable
      @click:close="submitError = undefined"
    >
      {{ submitError }}
    </v-alert>
  </div>
</template>
