<script setup lang="ts">
  // Vue & Store Imports
  import Placeholder from '@tiptap/extension-placeholder'
  import Underline from '@tiptap/extension-underline'
  import StarterKit from '@tiptap/starter-kit'
  import { EditorContent, useEditor } from '@tiptap/vue-3'
  import { BubbleMenu } from '@tiptap/vue-3/menus'
  import { Markdown } from 'tiptap-markdown'
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { useDisplay } from 'vuetify'

  // Component Imports

  // Props
  const props = defineProps<{
    /**
     * Placeholder text to display when the editor is empty.
     */
    placeholder?: string
    /**
     * Disables the editor, making it read-only.
     */
    disabled?: boolean
  }>()

  // Define Stores

  // Define Emits

  // Refs
  /**
   * `v-model` for the editor's content, synced as a Markdown string.
   */
  const content = defineModel<string>()
  /**
   * `v-model` to control which toolbar is displayed.
   * `true` shows the fixed toolbar, `false` shows the floating bubble menu.
   */
  const mountStyling = defineModel<boolean>('mountStyling', { default: false })
  /**
   * Holds the active inline format states (e.g., 'bold', 'italic') for the current selection.
   * Used to toggle the buttons in the toolbar.
   */
  const textFormatState = ref<string[]>([])
  /**
   * Holds the active block-level format state (e.g., 'h1', 'bulletList').
   * Used to toggle the buttons in the toolbar.
   */
  const blockState = ref<string>('')
  const { smAndDown } = useDisplay()
  const showFormattingToolbar = computed(() => mountStyling.value && !smAndDown.value)
  /**
   * The core Tiptap editor instance.
   */
  const editor = useEditor({
    content: content.value || '',
    editable: !props.disabled,
    extensions: [
      StarterKit,
      Placeholder.configure({
        placeholder: props.placeholder || 'How can I assist you today?',
      }),
      Underline,
      Markdown.configure({
        // Agent-facing composer: markdown string only, no raw HTML in v-model.
        html: false,
        tightLists: true,
        linkify: true,
        // Shift+Enter line breaks must survive round-trip as markdown, not a single paragraph.
        breaks: true,
      }),
    ],
    /**
     * When the editor content changes, emit the new content as Markdown.
     * If the editor is empty, emit an empty string for clean v-model handling.
     */
    onUpdate: ({ editor }) => {
      if (editor.isEmpty) {
        content.value = ''
      } else {
        content.value = (editor.storage as any).markdown.getMarkdown()
      }
    },
    /**
     * On every transaction (e.g., selection change, content change),
     * check the active formats and update the state for the toolbars.
     */
    onTransaction: ({ editor }) => {
      // Update text format state
      const formats: string[] = []
      if (editor.isActive('bold')) formats.push('bold')
      if (editor.isActive('italic')) formats.push('italic')
      if (editor.isActive('underline')) formats.push('underline')
      textFormatState.value = formats

      // Update block state
      if (editor.isActive('heading', { level: 1 })) {
        blockState.value = 'h1'
      } else if (editor.isActive('heading', { level: 2 })) {
        blockState.value = 'h2'
      } else if (editor.isActive('bulletList')) {
        blockState.value = 'bulletList'
      } else if (editor.isActive('orderedList')) {
        blockState.value = 'orderedList'
      } else if (editor.isActive('codeBlock')) {
        blockState.value = 'codeBlock'
      } else {
        blockState.value = ''
      }
    },
    editorProps: {
      attributes: {
        class: 'prose-mirror-editor tiptap-content',
      },
    },
  })

  // Computed

  // Watchers
  /**
   * Watch for external changes to the v-model (e.g., from a parent component)
   * and update the editor's content accordingly.
   */
  watch(
    () => content.value,
    (newVal) => {
      // Skip setContent when markdown already matches — avoids cursor reset on self-emit from onUpdate.
      if (editor.value && (editor.value.storage as any).markdown.getMarkdown() !== newVal) {
        editor.value.commands.setContent(newVal || '')
      }
    },
  )

  /**
   * Dynamically update the editor's placeholder if the prop changes.
   */
  watch(
    () => props.placeholder,
    (newPlaceholder) => {
      if (editor.value) {
        const placeholderExtension = editor.value.extensionManager.extensions.find((extension) => extension.name === 'placeholder')
        if (placeholderExtension) {
          placeholderExtension.options.placeholder = newPlaceholder || ''
          editor.value.view.dispatch(editor.value.state.tr)
        }
      }
    },
  )

  /**
   * Dynamically enable or disable the editor's editable state if the prop changes.
   */
  watch(
    () => props.disabled,
    (isDisabled) => {
      if (editor.value && editor.value.isEditable === isDisabled) {
        editor.value.setEditable(!isDisabled)
      }
    },
    { immediate: true },
  )

  // Functions

  // Lifecycle Hooks
  /**
   * Clean up the editor instance when the component is unmounted to prevent memory leaks.
   */
  onBeforeUnmount(() => {
    editor.value?.destroy()
  })
</script>

<template>
  <v-slide-y-reverse-transition>
    <div
      v-if="showFormattingToolbar"
      class="d-flex align-center"
    >
      <v-btn-toggle
        :model-value="textFormatState"
        density="comfortable"
        color="primary"
        variant="text"
        multiple
      >
        <v-btn
          variant="text"
          value="bold"
          icon
          @click="editor?.chain().focus().toggleBold().run()"
          size="small"
        >
          <v-icon color="primary">format_bold</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="italic"
          @click="editor?.chain().focus().toggleItalic().run()"
          size="small"
        >
          <v-icon color="primary">format_italic</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="underline"
          @click="editor?.chain().focus().toggleUnderline().run()"
          size="small"
        >
          <v-icon color="primary">format_underlined</v-icon>
        </v-btn>
      </v-btn-toggle>

      <v-divider
        vertical
        class="mx-2"
        inset
      ></v-divider>

      <!-- Block-level elements -->
      <v-btn-toggle
        :model-value="blockState"
        density="comfortable"
        color="primary"
        variant="text"
      >
        <v-btn
          icon
          value="h1"
          variant="text"
          @click="editor?.chain().focus().toggleHeading({ level: 1 }).run()"
          size="small"
        >
          <span class="text-label-medium font-weight-bold">H1</span>
        </v-btn>
        <v-btn
          icon
          value="h2"
          variant="text"
          @click="editor?.chain().focus().toggleHeading({ level: 2 }).run()"
          size="small"
        >
          <span class="text-label-medium font-weight-bold">H2</span>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="bulletList"
          @click="editor?.chain().focus().toggleBulletList().run()"
          size="small"
        >
          <v-icon color="primary">format_list_bulleted</v-icon>
        </v-btn>
        <v-btn
          variant="text"
          value="orderedList"
          @click="editor?.chain().focus().toggleOrderedList().run()"
          size="small"
        >
          <v-icon color="primary">format_list_numbered</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="codeBlock"
          @click="editor?.chain().focus().toggleCodeBlock().run()"
          size="small"
        >
          <v-icon color="primary">code</v-icon>
        </v-btn>
      </v-btn-toggle>
    </div>
  </v-slide-y-reverse-transition>
  <BubbleMenu
    v-if="editor"
    :editor="editor"
    :should-show="({ view, from, to }) => view.hasFocus() && from !== to && !showFormattingToolbar"
    style="z-index: 2"
  >
    <v-card
      class="pa-1 d-flex overflow-x-auto opacity-1 bg-white"
      rounded="pill"
      color="white"
      elevation="4"
    >
      <!-- Bold, Italic, Underline -->
      <v-btn-toggle
        :model-value="textFormatState"
        color="primary"
        variant="text"
        multiple
        density="compact"
      >
        <v-btn
          icon
          variant="text"
          value="bold"
          @click="editor?.chain().focus().toggleBold().run()"
          size="x-small"
        >
          <v-icon color="primary">format_bold</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="italic"
          @click="editor?.chain().focus().toggleItalic().run()"
          size="x-small"
        >
          <v-icon color="primary">format_italic</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="underline"
          @click="editor?.chain().focus().toggleUnderline().run()"
          size="x-small"
        >
          <v-icon color="primary">format_underlined</v-icon>
        </v-btn>
      </v-btn-toggle>
      <v-divider
        vertical
        class="mx-2"
      ></v-divider>

      <!-- Block-level elements -->
      <v-btn-toggle
        :model-value="blockState"
        color="primary"
        variant="text"
        density="compact"
      >
        <v-btn
          value="h1"
          icon
          variant="text"
          @click="editor?.chain().focus().toggleHeading({ level: 1 }).run()"
          size="x-small"
        >
          <span class="text-label-medium font-weight-bold">H1</span>
        </v-btn>
        <v-btn
          icon
          value="h2"
          variant="text"
          @click="editor?.chain().focus().toggleHeading({ level: 2 }).run()"
          size="x-small"
        >
          <span class="text-label-medium font-weight-bold">H2</span>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="bulletList"
          @click="editor?.chain().focus().toggleBulletList().run()"
          size="x-small"
        >
          <v-icon color="primary">format_list_numbered</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="orderedList"
          @click="editor?.chain().focus().toggleOrderedList().run()"
          size="x-small"
        >
          <v-icon color="primary">format_list_numbered</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="text"
          value="codeBlock"
          @click="editor?.chain().focus().toggleCodeBlock().run()"
          size="x-small"
        >
          <v-icon color="primary">code</v-icon>
        </v-btn>
      </v-btn-toggle>
    </v-card>
  </BubbleMenu>
  <EditorContent :editor="editor" />
</template>

<style scoped>
  :deep(.prose-mirror-editor) {
    outline: none;
    width: 100%;
    min-height: 62px;
    position: relative;
    padding: 12px;
    border-radius: 0;
    line-height: 1.5rem;
    max-height: 280px;
    overflow-y: auto;
  }

  :deep(.prose-mirror-editor p.is-editor-empty:first-child) {
    position: relative;
  }

  :deep(.prose-mirror-editor p.is-editor-empty:first-child::before) {
    content: attr(data-placeholder);
    float: left;
    margin-left: 24px;
    color: rgb(148, 148, 148);
    pointer-events: none;
    height: 0;
  }

  :deep(.prose-mirror-editor p.is-editor-empty:first-child::after) {
    content: '';
    position: absolute;
    left: 0;
    top: 0.18rem;
    width: 16px;
    height: 16px;
    opacity: 0.6;
    background-color: currentColor;
    pointer-events: none;
    mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Cpath fill='black' d='M12 1l8 4v6c0 5.55-3.84 10.74-8 12-4.16-1.26-8-6.45-8-12V5l8-4zm0 2.18L6 6.18V11c0 4.52 2.98 8.93 6 10.11 3.02-1.18 6-5.59 6-10.11V6.18l-6-3zm0 2.82a3 3 0 013 3c0 1.31-.83 2.42-2 2.83V15h-2v-3.17A2.99 2.99 0 019 9a3 3 0 013-3zm0 2a1 1 0 100 2 1 1 0 000-2z'/%3E%3C/svg%3E")
      no-repeat center / contain;
    -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Cpath fill='black' d='M12 1l8 4v6c0 5.55-3.84 10.74-8 12-4.16-1.26-8-6.45-8-12V5l8-4zm0 2.18L6 6.18V11c0 4.52 2.98 8.93 6 10.11 3.02-1.18 6-5.59 6-10.11V6.18l-6-3zm0 2.82a3 3 0 013 3c0 1.31-.83 2.42-2 2.83V15h-2v-3.17A2.99 2.99 0 019 9a3 3 0 013-3zm0 2a1 1 0 100 2 1 1 0 000-2z'/%3E%3C/svg%3E")
      no-repeat center / contain;
  }
</style>
