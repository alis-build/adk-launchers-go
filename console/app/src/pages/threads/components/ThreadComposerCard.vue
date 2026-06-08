<script setup lang="ts">
  import ContentInput from '@/components/ContentInput/ContentInput.vue'
  import type { MessagePart } from '@/components/ContentInput/types'

  defineProps<{
    /** Disables input and submit. */
    disabled?: boolean
    /** Shows stop control while the agent is streaming. */
    loading?: boolean
    /** Max width of the composer card (CSS length). */
    maxWidth?: string | number
    placeholder?: string
  }>()

  const emit = defineEmits<{
    /** Composer submitted message parts. */
    submit: [parts: MessagePart[]]
    /** User stopped an in-flight agent run. */
    stop: []
  }>()
</script>

<template>
  <v-card
    class="thread-composer-card w-100"
    :max-width="maxWidth ?? '770px'"
  >
    <ContentInput
      :disabled="disabled"
      :loading="loading"
      :placeholder="placeholder"
      @submit="emit('submit', $event)"
      @stop="emit('stop')"
    />
  </v-card>
</template>

<style scoped lang="scss">
  .thread-composer-card {
    overflow: hidden;
  }
</style>
