<template>
  <v-card
    variant="flat"
    rounded="xl"
    class="composer-shell pt-4 w-100 border-thin"
    :class="[dropZoneClass, { 'composer-card--compact-mobile': compactMobileComposer, 'composer-shell--dark': isDarkTheme }]"
    @dragenter="handleDragEnter"
    @dragleave="handleDragLeave"
    @dragover.prevent="handleDragOver"
    @drop.prevent="handleDrop"
    @paste="handlePaste"
  >
    <div
      v-if="isDragging"
      class="drop-zone-overlay d-flex flex-column align-center justify-center position-absolute w-100 h-100 top-0 left-0 text-primary rounded-xl"
    >
      <div class="mb-2">
        <v-icon>attach_file</v-icon>
      </div>
      Drop files here.
    </div>

    <div
      v-if="uploadedFiles && uploadedFiles.length > 0"
      class="px-4 pb-2"
    >
      <FileUploader
        v-model="uploadedFiles"
        :disabled="disabled"
        @remove-file="removeFile"
      />
    </div>
    <div class="editor-wrapper px-5">
      <RichTextInput
        v-model="textInput"
        v-model:mount-styling="mountTextStyling"
        :placeholder="placeholder"
        :disabled="disabled"
      />
    </div>
    <v-card-actions
      class="composer-actions px-2"
      :class="{ 'composer-actions--compact-mobile': compactMobileComposer }"
    >
      <div class="d-flex align-center ga-1 flex-wrap">
        <v-btn
          variant="text"
          rounded="xl"
          icon
          class="composer-tool-btn"
          @click="fileInputComponent?.click()"
          :disabled="disabled"
        >
          <v-icon>add</v-icon>
          <v-tooltip activator="parent"> Upload a file </v-tooltip>
        </v-btn>

        <v-file-input
          v-show="false"
          v-model="uploadedFiles"
          multiple
          :disabled="disabled"
          ref="fileInputComponent"
        />
      </div>

      <v-spacer />

      <div class="d-flex align-center ga-2">
        <v-btn
          v-if="loading"
          variant="tonal"
          icon
          @click="emit('stop')"
        >
          <v-icon>stop</v-icon>
          <v-tooltip activator="parent"> Stop response </v-tooltip>
        </v-btn>
        <v-btn
          v-else-if="hasContent"
          color="primary"
          variant="flat"
          rounded="xl"
          icon
          @click="submit"
        >
          <v-icon>send</v-icon>
          <v-tooltip activator="parent"> Send message </v-tooltip>
        </v-btn>
      </div>
    </v-card-actions>
  </v-card>
</template>

<script setup lang="ts">
  import { useMagicKeys } from '@vueuse/core'
  import type { Ref } from 'vue'
  import { computed, ref, watchEffect } from 'vue'
  import { useDisplay, useTheme } from 'vuetify'
  import { VFileInput } from 'vuetify/components'
  import { FileMessagePart, MessagePart, TextMessagePart } from './types.ts'

  import FileUploader from './FileUploader.vue'
  import RichTextInput from './RichTextInput.vue'

  const props = defineProps<{
    /** Placeholder for the rich-text editor. */
    placeholder?: string
    /** Disables editing and submit. */
    disabled?: boolean
    /** Shows stop control while the agent streams. */
    loading?: boolean
  }>()

  interface Emits {
    /** User submitted composed parts (text, files). */
    (e: 'submit', parts: MessagePart[]): void
    /** User requested to abort the active agent run. */
    (e: 'stop'): void
  }

  const emit = defineEmits<Emits>()

  /** Current markdown text from the rich-text editor. */
  const textInput: Ref<string> = ref('')
  /** Files selected via file picker, drag-and-drop, or paste. */
  const uploadedFiles: Ref<File[] | undefined> = ref(undefined)
  /** Reference to the hidden VFileInput component for programmatic click. */
  const fileInputComponent: Ref<VFileInput | null> = ref(null)
  /** Counter for nested drag enter/leave events to track drop zone state. */
  const dragCounter: Ref<number> = ref(0)

  const mountTextStyling: Ref<boolean> = ref(false)
  const { smAndDown } = useDisplay()
  const theme = useTheme()

  watchEffect(() => {
    if (textInput.value.trim()) {
      mountTextStyling.value = true
    } else {
      mountTextStyling.value = false
    }
  })

  // Computed
  const isDragging = computed(() => dragCounter.value > 0)

  const dropZoneClass = computed(() => {
    return {
      'composer-shell--dragging': isDragging.value,
    }
  })

  const hasContent = computed(() => {
    return (textInput.value && textInput.value.trim().length > 0) || (uploadedFiles.value && uploadedFiles.value.length > 0)
  })

  const compactMobileComposer = computed(() => smAndDown.value && textInput.value.trim().length > 0)
  const isDarkTheme = computed(() => theme.global.current.value.dark)

  // Watchers
  const { meta_enter, ctrl_enter } = useMagicKeys()
  watchEffect(async () => {
    // Ctrl+Enter / Cmd+Enter submits while plain Enter remains a newline in the editor.
    if (meta_enter?.value || ctrl_enter?.value) {
      if (!props.loading && hasContent.value) {
        await submit()
      }
    }
  })

  /**
   * Removes a file from the uploaded files list by name and size.
   * @param fileToRemove - The file to remove.
   */
  function removeFile(fileToRemove: File) {
    if (!uploadedFiles.value) return
    const index = uploadedFiles.value.findIndex((f) => f.name === fileToRemove.name && f.size === fileToRemove.size)
    if (index > -1) {
      uploadedFiles.value.splice(index, 1)
    }
    if (uploadedFiles.value.length === 0) {
      uploadedFiles.value = undefined
    }
  }

  function handleDragEnter(event: DragEvent) {
    event.preventDefault()
    dragCounter.value++
  }

  function handleDragLeave(event: DragEvent) {
    event.preventDefault()
    dragCounter.value--
  }

  function handleDragOver(event: DragEvent) {
    event.preventDefault()
  }

  function handleDrop(event: DragEvent) {
    event.preventDefault()
    dragCounter.value = 0
    if (event.dataTransfer && event.dataTransfer.files.length) {
      const files = Array.from(event.dataTransfer.files)
      const validFiles: File[] = []

      // Same dedupe rule as paste: name + size match skips re-adding an attachment.
      files.forEach((file) => {
        const isDuplicate = uploadedFiles.value?.some((existingFile) => existingFile.name === file.name && existingFile.size === file.size)
        if (!isDuplicate) {
          validFiles.push(file)
        }
      })

      if (validFiles.length > 0) {
        if (!uploadedFiles.value) {
          uploadedFiles.value = []
        }
        uploadedFiles.value.push(...validFiles)
      }
    }
  }

  function handlePaste(event: ClipboardEvent) {
    if (!event.clipboardData) return

    const items = event.clipboardData.items
    const validFiles: File[] = []
    let mediaPasted = false

    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      if (item?.kind === 'file') {
        const file = item.getAsFile()
        if (file) {
          mediaPasted = true
          // Clipboard files often share names (e.g. image.png); prefix avoids upload collisions.
          const randomString = Math.random().toString(36).substring(2, 10)
          const newFile = new File([file], `${randomString}_${file.name}`, {
            type: file.type,
            lastModified: file.lastModified,
          })
          validFiles.push(newFile)
        }
      }
    }

    if (mediaPasted) {
      // Only intercept paste when files are present so plain text still reaches the editor.
      event.preventDefault()
      if (validFiles.length > 0) {
        if (!uploadedFiles.value) {
          uploadedFiles.value = []
        }
        uploadedFiles.value.push(...validFiles)
      }
    }
  }

  /**
   * Collects all content (text + files) into MessagePart objects,
   * emits them via the `submit` event, and resets the input state.
   */
  async function submit() {
    if (props.loading) return

    const parts: MessagePart[] = []

    // Add text part if text exists
    if (textInput.value && textInput.value.trim().length > 0) {
      parts.push(new TextMessagePart(textInput.value.trim()))
    }

    // Add file parts
    if (uploadedFiles.value && uploadedFiles.value.length > 0) {
      for (const file of uploadedFiles.value) {
        parts.push(new FileMessagePart(file))
      }
    }

    if (parts.length > 0) {
      emit('submit', parts)
      // Reset all inputs
      textInput.value = ''
      uploadedFiles.value = undefined
    }
  }

  // Lifecycle Hooks
</script>

<style scoped lang="scss">
  .composer-shell--dragging {
    border-color: rgba(var(--v-theme-primary), 0.45) !important;
    background: rgba(var(--v-theme-primary), 0.05) !important;
  }

  .composer-actions {
    min-height: 56px;
  }

  .composer-tool-btn,
  .composer-mode-btn {
    color: rgba(var(--v-theme-on-surface), 0.72);
  }

  .composer-card--compact-mobile {
    padding-top: 0.75rem !important;
  }

  .composer-actions--compact-mobile {
    min-height: 48px;
    padding-top: 0 !important;
  }

  .composer-shell--dark {
    background: #1f1f1f !important;
    border-color: rgba(255, 255, 255, 0.12) !important;
  }
</style>

<style lang="scss" scoped>
  .drop-zone-overlay {
    background-color: rgba(var(--v-theme-surface), 0.96);
    border: 2px dashed rgb(var(--v-theme-primary));
    z-index: 1;
  }
</style>
