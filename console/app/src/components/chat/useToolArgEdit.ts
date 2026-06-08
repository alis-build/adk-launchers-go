/**
 * Composable for editing tool call arguments as JSON before confirmation.
 *
 * @module components/chat/useToolArgEdit
 */
import type { ChatToolCall, ToolConfirmationPayload } from '@/pages/threads/types'
import { computed, ref, watch, type Ref } from 'vue'

/** Manages a JSON text editor for a tool call's arguments. */
export function useToolArgEdit(toolCall: Ref<ChatToolCall | undefined>) {
  const showEditMode = ref(false)
  const editedArgsJson = ref('')
  const jsonError = ref('')

  const isValidJson = computed(() => !jsonError.value && !!editedArgsJson.value.trim())

  function resetToOriginal() {
    if (toolCall.value?.args) {
      editedArgsJson.value = JSON.stringify(toolCall.value.args, null, 2)
      jsonError.value = ''
    }
  }

  function validateJson() {
    try {
      const parsed = JSON.parse(editedArgsJson.value)
      if (parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)) {
        jsonError.value = ''
      } else {
        jsonError.value = 'Must be a JSON object'
      }
    } catch {
      jsonError.value = 'Invalid JSON syntax'
    }
  }

  function buildPayload(approved: boolean, withEdits: boolean): ToolConfirmationPayload | undefined {
    if (!approved) return { approved: false }
    const payload: ToolConfirmationPayload = { approved: true }
    if (withEdits) {
      if (!isValidJson.value) return undefined
      try {
        payload.editedArgs = JSON.parse(editedArgsJson.value) as Record<string, unknown>
      } catch {
        jsonError.value = 'Invalid JSON'
        return undefined
      }
    }
    return payload
  }

  watch(
    toolCall,
    (tc) => {
      if (tc?.args) resetToOriginal()
    },
    { immediate: true },
  )

  return {
    showEditMode,
    editedArgsJson,
    jsonError,
    isValidJson,
    resetToOriginal,
    validateJson,
    buildPayload,
  }
}
