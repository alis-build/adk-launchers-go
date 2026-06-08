<script setup lang="ts">
  /** Permission card for a single tool confirmation — Allow & run / Deny with optional arg editing. */
  import ExpirationTimer from '@/components/chat/ExpirationTimer.vue'
  import { formatArgsInline, getInterruptRiskTier, getInterruptSubtitle, getInterruptTitle, riskTierLabel } from '@/components/chat/toolInterruptDisplay'
  import { buildSingleResumeEntry } from '@/components/chat/toolInterruptResume'
  import { useToolArgEdit } from '@/components/chat/useToolArgEdit'
  import type { ChatToolCall, Interrupt, ResumeEntry, ToolConfirmationPayload } from '@/pages/threads/types'
  import { computed, ref, toRef } from 'vue'

  const props = defineProps<{
    interrupt: Interrupt
    toolCall?: ChatToolCall
    disabled?: boolean
  }>()

  const emit = defineEmits<{
    (e: 'submit-resume', resumeEntries: ResumeEntry[]): void
    (e: 'expired'): void
  }>()

  const showDetails = ref(false)

  const canEdit = computed(() => {
    const schema = props.interrupt.responseSchema as Record<string, unknown> | undefined
    const propsSchema = schema?.properties as Record<string, unknown> | undefined
    return propsSchema?.editedArgs !== undefined
  })

  const title = computed(() => getInterruptTitle(props.interrupt, props.toolCall))
  const subtitle = computed(() => getInterruptSubtitle(props.interrupt, props.toolCall))
  const inlineArgs = computed(() => formatArgsInline(props.toolCall?.args))
  const riskTier = computed(() => getInterruptRiskTier(props.interrupt))

  const toolCallRef = toRef(props, 'toolCall')
  const { showEditMode, editedArgsJson, jsonError, isValidJson, resetToOriginal, validateJson, buildPayload } = useToolArgEdit(toolCallRef)

  function submitPayload(payload: ToolConfirmationPayload) {
    emit('submit-resume', buildSingleResumeEntry(props.interrupt.id, payload))
  }

  function allowAndRun(withEdits: boolean) {
    const payload = buildPayload(true, withEdits)
    if (payload) submitPayload(payload)
  }

  function deny() {
    submitPayload({ approved: false })
  }
</script>

<template>
  <v-card
    variant="outlined"
    class="tool-permission-card pa-4"
    :class="{ 'opacity-70': disabled }"
    data-test="tool-permission-card"
  >
    <div class="d-flex align-center mb-2">
      <span class="text-caption text-medium-emphasis text-uppercase mr-2">Permission</span>
      <v-chip
        v-if="riskTier"
        size="x-small"
        variant="tonal"
        class="mr-2"
      >
        {{ riskTierLabel(riskTier) }}
      </v-chip>
      <v-spacer />
      <ExpirationTimer
        v-if="interrupt.expiresAt"
        :expires-at="interrupt.expiresAt"
        @expired="emit('expired')"
      />
    </div>

    <p class="text-body-large font-weight-medium mb-1">
      {{ title }}
    </p>
    <p
      v-if="subtitle"
      class="text-body-small text-medium-emphasis mb-3"
    >
      {{ subtitle }}
    </p>

    <div
      v-if="toolCall && (inlineArgs || canEdit)"
      class="args-preview pa-2 mb-3 text-body-small"
    >
      <code v-if="inlineArgs">{{ inlineArgs }}</code>
      <v-btn
        v-if="toolCall.args && Object.keys(toolCall.args).length"
        variant="text"
        size="x-small"
        class="ml-1"
        @click="showDetails = !showDetails"
      >
        {{ showDetails ? 'hide details' : 'details' }}
      </v-btn>
      <v-btn
        v-if="canEdit && !showEditMode"
        variant="text"
        size="x-small"
        @click="showEditMode = true"
      >
        edit
      </v-btn>
      <pre
        v-if="showDetails"
        class="tool-args-full text-body-small mt-2"
        >{{ JSON.stringify(toolCall.args, null, 2) }}</pre
      >
    </div>

    <div v-if="!showEditMode">
      <div class="d-flex flex-wrap gap-2">
        <v-spacer />

        <v-btn
          color="error"
          variant="outlined"
          data-test="deny"
          class="mx-1"
          :disabled="disabled"
          @click="deny"
        >
          Deny
        </v-btn>
        <v-btn
          color="success"
          data-test="allow-and-run"
          :disabled="disabled"
          class="mx-1"
          @click="allowAndRun(false)"
        >
          Allow & run
        </v-btn>
      </div>
    </div>

    <div v-else>
      <p class="text-body-small text-medium-emphasis mb-2">
        Edit arguments (JSON), then allow or deny.
        <v-btn
          variant="text"
          size="small"
          @click="showEditMode = false"
        >
          Cancel edit
        </v-btn>
      </p>
      <v-textarea
        v-model="editedArgsJson"
        label="Tool arguments (JSON)"
        variant="outlined"
        density="compact"
        rows="6"
        :error-messages="jsonError"
        :disabled="disabled"
        class="mb-3"
        @input="validateJson"
      />
      <div class="d-flex flex-wrap gap-2">
        <v-btn
          color="success"
          :disabled="disabled || !isValidJson"
          @click="allowAndRun(true)"
        >
          Allow with changes & run
        </v-btn>
        <v-btn
          color="error"
          variant="outlined"
          :disabled="disabled"
          @click="deny"
        >
          Deny
        </v-btn>
        <v-btn
          variant="text"
          @click="resetToOriginal"
        >
          Reset
        </v-btn>
      </div>
    </div>
  </v-card>
</template>

<style lang="scss" scoped>
  .args-preview {
    background: rgba(var(--v-theme-on-surface), 0.05);
    border-radius: 4px;
  }

  .tool-args-full {
    background: rgba(var(--v-theme-on-surface), 0.05);
    padding: 12px;
    border-radius: 4px;
    overflow-x: auto;
    margin: 0;
  }
</style>
