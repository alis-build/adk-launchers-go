<script setup lang="ts">
  import type { Ref } from 'vue'
  import { computed, nextTick, onUnmounted, ref } from 'vue'

  const props = defineProps<{
    /** Disables starting a new recording. */
    disabled?: boolean
  }>()

  const emit = defineEmits<{
    (e: 'update:isRecording', value: boolean): void
    (e: 'update:audioBlob', value: Blob | undefined): void
  }>()

  // Refs
  const isRecording = defineModel<boolean>('isRecording', { default: false })
  const audioBlob = defineModel<Blob | undefined>('audioBlob')
  const CONTENT_TYPE_AUDIO = 'audio/mp4'
  const audioChunks = ref<Blob[]>([])
  const mediaStream = ref<MediaStream | undefined>(undefined)
  const mediaRecorder = ref<MediaRecorder | undefined>(undefined)
  const recordingTimer = ref<number | undefined>(undefined)
  const recordingDuration = ref(0)
  const audioPlayer = ref<HTMLAudioElement | undefined>(undefined)
  const audioUrl = ref<string | undefined>(undefined)
  const buttonRecord: Ref<boolean> = ref(false)
  const canvasRef = ref<HTMLCanvasElement | null>(null)
  const audioContext = ref<AudioContext | null>(null)
  const analyser = ref<AnalyserNode | null>(null)
  const sourceNode = ref<MediaStreamAudioSourceNode | null>(null)
  const dataArray = ref<Uint8Array | null>(null)
  const animationFrameId = ref<number | null>(null)
  const waveformBars = ref<number[]>([])
  const MAX_WAVEFORM_BARS = 1200

  // Computed
  const recordingDurationFormatted = computed(() => {
    const minutes = Math.floor(recordingDuration.value / 60)
    const remainingSeconds = recordingDuration.value % 60
    return `${minutes.toString().padStart(2, '0')}:${remainingSeconds.toString().padStart(2, '0')}`
  })

  // Watchers

  // Functions
  async function startRecording(btn?: boolean) {
    if (btn) {
      buttonRecord.value = true
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      mediaStream.value = stream

      // Analyser is separate from MediaRecorder: waveform needs time-domain samples while encoding runs.
      audioContext.value = new (window.AudioContext || (window as any).webkitAudioContext)()
      analyser.value = audioContext.value.createAnalyser()
      analyser.value.fftSize = 256
      const bufferLength = analyser.value.frequencyBinCount
      dataArray.value = new Uint8Array(bufferLength)
      sourceNode.value = audioContext.value.createMediaStreamSource(stream)
      sourceNode.value.connect(analyser.value)

      mediaRecorder.value = new MediaRecorder(stream)

      audioChunks.value = []
      audioBlob.value = undefined

      recordingDuration.value = 0

      recordingTimer.value = window.setInterval(() => {
        recordingDuration.value++
      }, 1000)

      // Setup event listeners
      mediaRecorder.value.ondataavailable = (event) => {
        if (event.data.size > 0) {
          audioChunks.value.push(event.data)
        }
      }

      mediaRecorder.value.onstop = () => {
        const blob = new Blob(audioChunks.value, { type: CONTENT_TYPE_AUDIO })
        audioBlob.value = blob
        emit('update:audioBlob', blob)
        if (audioUrl.value) {
          URL.revokeObjectURL(audioUrl.value)
        }
        audioUrl.value = URL.createObjectURL(blob)
        if (recordingTimer.value) {
          clearInterval(recordingTimer.value)
          recordingTimer.value = undefined
        }
        stopVisualization()
        buttonRecord.value = false
      }

      mediaRecorder.value.start()
      isRecording.value = true
      emit('update:isRecording', true)

      await nextTick()

      if (canvasRef.value) {
        const container = canvasRef.value.parentElement
        if (container) {
          canvasRef.value.width = container.clientWidth
          canvasRef.value.height = 80
        } else {
          canvasRef.value.width = 300
          canvasRef.value.height = 80
        }
        drawWaveform()
      }
    } catch (error) {
      console.error('Error accessing microphone:', error)
      buttonRecord.value = false
      alert('Unable to access microphone. Please check permissions.')
    }
  }

  // Function to stop the media stream and its tracks
  function clearMediaStream() {
    if (mediaStream.value) {
      mediaStream.value.getTracks().forEach((track) => track.stop())
      mediaStream.value = undefined
    }
  }

  // Function to stop recording
  function stopRecording() {
    if (mediaRecorder.value && isRecording.value) {
      buttonRecord.value = false
      mediaRecorder.value.stop()
      isRecording.value = false
      emit('update:isRecording', false)
      clearMediaStream()
    }
  }

  function deleteRecording() {
    audioBlob.value = undefined
    emit('update:audioBlob', undefined)
    if (audioUrl.value) {
      URL.revokeObjectURL(audioUrl.value)
      audioUrl.value = undefined
    }
    if (audioPlayer.value) {
      audioPlayer.value.src = ''
    }
    clearMediaStream()
  }

  // Waveform Drawing Function
  function drawWaveform() {
    if (!analyser.value || !canvasRef.value || !dataArray.value) {
      console.warn('Waveform drawing dependencies not ready.')
      return
    }

    const canvas = canvasRef.value
    const canvasCtx = canvas.getContext('2d')
    if (!canvasCtx) {
      console.error('Could not get canvas context')
      return
    }

    animationFrameId.value = requestAnimationFrame(drawWaveform)

    analyser.value.getByteTimeDomainData(dataArray.value as Uint8Array<ArrayBuffer>)

    let sumOfMagnitudes = 0
    for (let i = 0; i < dataArray.value.length; i++) {
      sumOfMagnitudes += Math.abs(dataArray.value[i]! - 128)
    }
    const averageMagnitude = sumOfMagnitudes / dataArray.value.length

    waveformBars.value.push(averageMagnitude)

    // Fixed bar count: shift left so the waveform scrolls right-to-left like a live meter.
    if (waveformBars.value.length > MAX_WAVEFORM_BARS) {
      waveformBars.value.shift()
    }

    const WIDTH = canvas.width
    const HEIGHT = canvas.height
    const BAR_SPACING = 2
    const TOTAL_BAR_WIDTH = WIDTH / MAX_WAVEFORM_BARS
    const BAR_WIDTH = Math.max(1, TOTAL_BAR_WIDTH - BAR_SPACING)
    const MAX_BAR_HEIGHT_FACTOR = 2

    canvasCtx.clearRect(0, 0, WIDTH, HEIGHT)
    canvasCtx.fillStyle = '#006383'

    for (let i = 0; i < waveformBars.value.length; i++) {
      const barHeightNormalized = waveformBars.value[i]! / 128
      let barHeight = Math.max(2, barHeightNormalized * HEIGHT * MAX_BAR_HEIGHT_FACTOR)
      barHeight = Math.min(barHeight, HEIGHT * MAX_BAR_HEIGHT_FACTOR)

      const x = WIDTH - (waveformBars.value.length - i) * TOTAL_BAR_WIDTH
      const y = (HEIGHT - barHeight) / 2

      canvasCtx.fillRect(x, y, BAR_WIDTH, barHeight)
    }
  }

  function stopVisualization() {
    // Cancel the animation loop
    if (animationFrameId.value) {
      cancelAnimationFrame(animationFrameId.value)
      animationFrameId.value = null
    }

    // Disconnect nodes
    sourceNode.value?.disconnect()
    sourceNode.value = null

    // Close the AudioContext
    if (audioContext.value && audioContext.value.state !== 'closed') {
      audioContext.value.close().catch(console.error)
    }
    audioContext.value = null
    analyser.value = null
    dataArray.value = null
    clearMediaStream()

    waveformBars.value = []
    const ctx = canvasRef.value?.getContext('2d')
    if (canvasRef.value) {
      ctx?.clearRect(0, 0, canvasRef.value.width, canvasRef.value.height)
    }
  }

  // Lifecycle Hooks
  onUnmounted(() => {
    clearMediaStream()
    stopVisualization()
    if (audioUrl.value) {
      URL.revokeObjectURL(audioUrl.value)
    }
  })
</script>

<template>
  <v-btn
    v-if="!isRecording && !audioBlob"
    variant="text"
    rounded="xl"
    icon
    :disabled="disabled"
    @click="startRecording(true)"
  >
    <v-icon>mic</v-icon>
    <v-tooltip activator="parent"> Record a voice memo </v-tooltip>
  </v-btn>
  <div
    v-else
    class="w-100 d-flex align-center"
  >
    <div
      v-if="isRecording"
      class="waveform-container mr-8 w-100 bg-grey-lighten-5 rounded-xl d-flex align-center px-4 text-body-medium"
    >
      <canvas
        ref="canvasRef"
        class="mr-4"
      />
      {{ recordingDurationFormatted }}
    </div>

    <v-btn
      v-if="isRecording"
      color="red-darken-1"
      variant="flat"
      icon
      @click="stopRecording"
    >
      <v-icon size="24">stop</v-icon>
      <v-tooltip activator="parent"> Stop the recording </v-tooltip>
    </v-btn>

    <!-- Ready for playback -->
    <div
      v-else-if="audioBlob"
      class="w-100 d-flex align-center"
    >
      <div class="w-100 d-flex align-center">
        <audio
          ref="audioPlayer"
          :src="audioUrl"
          class="audio-player rounded-xl flex-grow-1"
          controls
          playsinline
        />
        <v-btn
          size="large"
          variant="text"
          icon
          @click="deleteRecording"
        >
          <v-icon color="red-darken-2"> delete </v-icon>
          <v-tooltip activator="parent"> Delete the voice note </v-tooltip>
        </v-btn>
      </div>
    </div>
  </div>
</template>

<style scoped>
  .waveform-container {
    max-width: 100%;
    height: 56px;
    overflow: hidden;
  }

  canvas {
    display: block;
    width: 100%;
    height: 100%;
  }

  .audio-player {
    height: 56px;
    max-width: 80%;
  }
</style>
