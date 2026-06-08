<script setup lang="ts">
  /** Countdown timer that emits `expired` when an interrupt's deadline passes. */
  import { computed, onMounted, onUnmounted, ref } from 'vue'

  const props = defineProps<{
    expiresAt: string
  }>()

  const emit = defineEmits<{
    (e: 'expired'): void
  }>()

  const now = ref(new Date())
  let interval: ReturnType<typeof setInterval> | undefined

  const expiresAtDate = computed(() => new Date(props.expiresAt))
  const timeRemaining = computed(() =>
    Math.max(0, expiresAtDate.value.getTime() - now.value.getTime()),
  )
  const isExpired = computed(() => timeRemaining.value === 0)
  const isWarning = computed(() => timeRemaining.value > 0 && timeRemaining.value < 5 * 60 * 1000)

  const timeDisplay = computed(() => {
    if (isExpired.value) return 'Expired'
    const seconds = Math.floor(timeRemaining.value / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    if (hours > 0) return `${hours}h ${minutes % 60}m`
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`
    return `${seconds}s`
  })

  const timerIcon = computed(() => {
    if (isExpired.value) return 'mdi-timer-off'
    if (isWarning.value) return 'mdi-timer-alert'
    return 'mdi-timer'
  })

  onMounted(() => {
    interval = setInterval(() => {
      const wasExpired = isExpired.value
      now.value = new Date()
      if (!wasExpired && isExpired.value) {
        emit('expired')
      }
    }, 1000)
  })

  onUnmounted(() => {
    if (interval) clearInterval(interval)
  })
</script>

<template>
  <div
    class="expiration-timer d-flex align-center text-body-small"
    :class="{ 'text-warning': isWarning, 'text-error font-weight-medium': isExpired }"
  >
    <v-icon
      :icon="timerIcon"
      size="small"
      class="mr-1"
    />
    <span>{{ timeDisplay }}</span>
  </div>
</template>
