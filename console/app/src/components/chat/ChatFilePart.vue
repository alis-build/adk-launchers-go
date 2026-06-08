<script setup lang="ts">
  import type { ChatFile } from '@/pages/threads/types'
  import { computed } from 'vue'

  const props = defineProps<{
    /** Attachment metadata (name, mime, url or inline bytes). */
    file: ChatFile
  }>()

  const fileUri = computed(() => props.file.url ?? '')
  const fileBytes = computed(() => props.file.bytes ?? null)
  const mimeType = computed(() => props.file.mimeType)
  const fileName = computed(() => props.file.filename)

  const hasUri = computed(() => !!fileUri.value)
  const hasBytes = computed(() => !!fileBytes.value)

  const isImage = computed(() => {
    const mime = mimeType.value
    return mime ? mime.startsWith('image/') : false
  })

  const imageDataUrl = computed(() => {
    if (hasUri.value && fileUri.value) {
      return fileUri.value
    }

    if (!hasBytes.value) return ''
    const bytes = fileBytes.value
    if (!bytes || bytes.length === 0) return ''

    const mime = mimeType.value || 'image/png'

    const isLikelyBase64 = bytes.length > 0 && bytes.slice(0, 20).every((b) => b >= 32 && b <= 126)

    if (isLikelyBase64) {
      const chunkSize = 8192
      let base64String = ''
      for (let i = 0; i < bytes.length; i += chunkSize) {
        const chunk = bytes.slice(i, i + chunkSize)
        base64String += String.fromCharCode.apply(null, Array.from(chunk))
      }
      return `data:${mime};base64,${base64String}`
    }

    const chunkSize = 8192
    let binaryString = ''
    for (let i = 0; i < bytes.length; i += chunkSize) {
      const chunk = bytes.slice(i, i + chunkSize)
      binaryString += String.fromCharCode.apply(null, Array.from(chunk))
    }

    const base64 = btoa(binaryString)
    return `data:${mime};base64,${base64}`
  })

  const downloadFile = () => {
    if (hasUri.value && fileUri.value) {
      window.open(fileUri.value, '_blank')
    }
  }
</script>

<template>
  <div class="chat-file-part">
    <v-img
      v-if="isImage && hasUri"
      :src="fileUri"
      width="300"
      height="auto"
      cover
      class="my-4 rounded-xl"
    />
    <v-img
      v-else-if="isImage && hasBytes"
      :src="imageDataUrl"
      width="300"
      height="auto"
      cover
      class="my-4 rounded-xl"
    />
    <v-card
      v-else
      class="my-4"
    >
      <v-card-text>
        <div class="d-flex align-center">
          <v-icon class="mr-3"> description </v-icon>
          <div class="flex-grow-1">
            <div class="font-weight-bold">
              {{ fileName }}
            </div>
            <div
              v-if="mimeType"
              class="text-body-small text-grey"
            >
              {{ mimeType }}
            </div>
          </div>
          <v-btn
            v-if="hasUri"
            icon
            variant="text"
            size="small"
            @click="downloadFile"
          >
            <v-icon>download</v-icon>
          </v-btn>
        </div>
      </v-card-text>
    </v-card>
  </div>
</template>

<style lang="scss" scoped></style>
