<template>
  <svg
    v-if="variant === 'star'"
    class="agent-working-indicator"
    viewBox="0 0 128 128"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <defs>
      <linearGradient
        id="star-gradient-base"
        x1="-33"
        y1="26"
        x2="31"
        y2="-28"
        gradientUnits="userSpaceOnUse"
      >
        <stop
          offset="0%"
          stop-color="rgb(52,107,241)"
        />
        <stop
          offset="22%"
          stop-color="rgb(50,121,248)"
        />
        <stop
          offset="45%"
          stop-color="rgb(49,134,255)"
        />
        <stop
          offset="72%"
          stop-color="rgb(64,147,255)"
        />
        <stop
          offset="99%"
          stop-color="rgb(79,160,255)"
        />
      </linearGradient>
    </defs>
    <path
      class="star-core"
      fill="url(#star-gradient-base)"
      d="M-3.9,-84.95 C-5.28,-79.47 -7.08,-74.14 -9.32,-68.93 C-15.16,-55.37 -23.16,-43.5 -33.33,-33.33 C-43.5,-23.17 -55.37,-15.16 -68.93,-9.32 C-74.13,-7.08 -79.47,-5.28 -84.95,-3.9 C-86.74,-3.45 -88,-1.85 -88,0 C-88,1.85 -86.74,3.45 -84.95,3.9 C-79.47,5.28 -74.14,7.08 -68.93,9.32 C-55.37,15.16 -43.51,23.16 -33.33,33.33 C-23.16,43.5 -15.15,55.37 -9.32,68.93 C-7.08,74.13 -5.28,79.47 -3.9,84.95 C-3.45,86.74 -1.84,88 0,88 C1.85,88 3.45,86.74 3.9,84.95 C5.28,79.47 7.08,74.14 9.32,68.93 C15.16,55.37 23.16,43.51 33.33,33.33 C43.5,23.16 55.37,15.15 68.93,9.32 C74.13,7.08 79.47,5.28 84.95,3.9 C86.74,3.45 88,1.84 88,0 C88,-1.85 86.74,-3.45 84.95,-3.9 C79.47,-5.28 74.14,-7.08 68.93,-9.32 C55.37,-15.16 43.51,-23.16 33.33,-33.33 C23.16,-43.5 15.15,-55.37 9.32,-68.93 C7.08,-74.13 5.28,-79.47 3.9,-84.95 C3.45,-86.74 1.85,-88 0,-88 C-1.85,-88 -3.45,-86.74 -3.9,-84.95 Z"
      transform="translate(64 64) scale(0.4)"
    />
  </svg>

  <svg
    v-else-if="variant === 'star-error'"
    class="agent-working-indicator"
    viewBox="0 0 128 128"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <defs>
      <linearGradient
        id="star-gradient-error"
        x1="-33"
        y1="26"
        x2="31"
        y2="-28"
        gradientUnits="userSpaceOnUse"
      >
        <stop
          offset="0%"
          stop-color="rgb(241,52,52)"
        />
        <stop
          offset="22%"
          stop-color="rgb(248,50,50)"
        />
        <stop
          offset="45%"
          stop-color="rgb(255,49,49)"
        />
        <stop
          offset="72%"
          stop-color="rgb(255,64,64)"
        />
        <stop
          offset="99%"
          stop-color="rgb(255,79,79)"
        />
      </linearGradient>
    </defs>
    <path
      class="star-core"
      fill="url(#star-gradient-error)"
      d="M-3.9,-84.95 C-5.28,-79.47 -7.08,-74.14 -9.32,-68.93 C-15.16,-55.37 -23.16,-43.5 -33.33,-33.33 C-43.5,-23.17 -55.37,-15.16 -68.93,-9.32 C-74.13,-7.08 -79.47,-5.28 -84.95,-3.9 C-86.74,-3.45 -88,-1.85 -88,0 C-88,1.85 -86.74,3.45 -84.95,3.9 C-79.47,5.28 -74.14,7.08 -68.93,9.32 C-55.37,15.16 -43.51,23.16 -33.33,33.33 C-23.16,43.5 -15.15,55.37 -9.32,68.93 C-7.08,74.13 -5.28,79.47 -3.9,84.95 C-3.45,86.74 -1.84,88 0,88 C1.85,88 3.45,86.74 3.9,84.95 C5.28,79.47 7.08,74.14 9.32,68.93 C15.16,55.37 23.16,43.51 33.33,33.33 C43.5,23.16 55.37,15.15 68.93,9.32 C74.13,7.08 79.47,5.28 84.95,3.9 C86.74,3.45 88,1.84 88,0 C88,-1.85 86.74,-3.45 84.95,-3.9 C79.47,-5.28 74.14,-7.08 68.93,-9.32 C55.37,-15.16 43.51,-23.16 33.33,-33.33 C23.16,-43.5 15.15,-55.37 9.32,-68.93 C7.08,-74.13 5.28,-79.47 3.9,-84.95 C3.45,-86.74 1.85,-88 0,-88 C-1.85,-88 -3.45,-86.74 -3.9,-84.95 Z"
      transform="translate(64 64) scale(0.4)"
    />
  </svg>

  <svg
    v-else-if="variant === 'star-orbit-comet-5'"
    class="agent-working-indicator"
    viewBox="0 0 128 128"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <defs>
      <linearGradient
        id="star-gradient-comet-5"
        x1="-33"
        y1="26"
        x2="31"
        y2="-28"
        gradientUnits="userSpaceOnUse"
      >
        <stop
          offset="0%"
          stop-color="rgb(52,107,241)"
        />
        <stop
          offset="22%"
          stop-color="rgb(50,121,248)"
        />
        <stop
          offset="45%"
          stop-color="rgb(49,134,255)"
        />
        <stop
          offset="72%"
          stop-color="rgb(64,147,255)"
        />
        <stop
          offset="99%"
          stop-color="rgb(79,160,255)"
        />
      </linearGradient>
    </defs>
    <path
      class="star-core"
      fill="url(#star-gradient-comet-5)"
      d="M-3.9,-84.95 C-5.28,-79.47 -7.08,-74.14 -9.32,-68.93 C-15.16,-55.37 -23.16,-43.5 -33.33,-33.33 C-43.5,-23.17 -55.37,-15.16 -68.93,-9.32 C-74.13,-7.08 -79.47,-5.28 -84.95,-3.9 C-86.74,-3.45 -88,-1.85 -88,0 C-88,1.85 -86.74,3.45 -84.95,3.9 C-79.47,5.28 -74.14,7.08 -68.93,9.32 C-55.37,15.16 -43.51,23.16 -33.33,33.33 C-23.16,43.5 -15.15,55.37 -9.32,68.93 C-7.08,74.13 -5.28,79.47 -3.9,84.95 C-3.45,86.74 -1.84,88 0,88 C1.85,88 3.45,86.74 3.9,84.95 C5.28,79.47 7.08,74.14 9.32,68.93 C15.16,55.37 23.16,43.51 33.33,33.33 C43.5,23.16 55.37,15.15 68.93,9.32 C74.13,7.08 79.47,5.28 84.95,3.9 C86.74,3.45 88,1.84 88,0 C88,-1.85 86.74,-3.45 84.95,-3.9 C79.47,-5.28 74.14,-7.08 68.93,-9.32 C55.37,-15.16 43.51,-23.16 33.33,-33.33 C23.16,-43.5 15.15,-55.37 9.32,-68.93 C7.08,-74.13 5.28,-79.47 3.9,-84.95 C3.45,-86.74 1.85,-88 0,-88 C-1.85,-88 -3.45,-86.74 -3.9,-84.95 Z"
      transform="translate(64 64) scale(0.4)"
    />
    <g class="orbit-dots orbit-dots-comet-reverse">
      <circle
        class="orbit-dot orbit-dot-faint"
        cx="64"
        cy="104"
        r="4.1"
      />
    </g>
    <g class="orbit-dots orbit-dots-comet">
      <circle
        class="orbit-dot orbit-dot-secondary"
        cx="60"
        cy="20"
        r="4.9"
      />
      <circle
        class="orbit-dot orbit-dot-primary"
        cx="68"
        cy="15"
        r="7.4"
      />
    </g>
  </svg>

  <svg
    v-else
    class="agent-working-indicator"
    viewBox="0 0 128 128"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <defs>
      <linearGradient
        id="star-gradient-comet-5-reverse"
        x1="-33"
        y1="26"
        x2="31"
        y2="-28"
        gradientUnits="userSpaceOnUse"
      >
        <stop
          offset="0%"
          stop-color="rgb(52,107,241)"
        />
        <stop
          offset="22%"
          stop-color="rgb(50,121,248)"
        />
        <stop
          offset="45%"
          stop-color="rgb(49,134,255)"
        />
        <stop
          offset="72%"
          stop-color="rgb(64,147,255)"
        />
        <stop
          offset="99%"
          stop-color="rgb(79,160,255)"
        />
      </linearGradient>
    </defs>
    <path
      class="star-core"
      fill="url(#star-gradient-comet-5-reverse)"
      d="M-3.9,-84.95 C-5.28,-79.47 -7.08,-74.14 -9.32,-68.93 C-15.16,-55.37 -23.16,-43.5 -33.33,-33.33 C-43.5,-23.17 -55.37,-15.16 -68.93,-9.32 C-74.13,-7.08 -79.47,-5.28 -84.95,-3.9 C-86.74,-3.45 -88,-1.85 -88,0 C-88,1.85 -86.74,3.45 -84.95,3.9 C-79.47,5.28 -74.14,7.08 -68.93,9.32 C-55.37,15.16 -43.51,23.16 -33.33,33.33 C-23.16,43.5 -15.15,55.37 -9.32,68.93 C-7.08,74.13 -5.28,79.47 -3.9,84.95 C-3.45,86.74 -1.84,88 0,88 C1.85,88 3.45,86.74 3.9,84.95 C5.28,79.47 7.08,74.14 9.32,68.93 C15.16,55.37 23.16,43.51 33.33,33.33 C43.5,23.16 55.37,15.15 68.93,9.32 C74.13,7.08 79.47,5.28 84.95,3.9 C86.74,3.45 88,1.84 88,0 C88,-1.85 86.74,-3.45 84.95,-3.9 C79.47,-5.28 74.14,-7.08 68.93,-9.32 C55.37,-15.16 43.51,-23.16 33.33,-33.33 C23.16,-43.5 15.15,-55.37 9.32,-68.93 C7.08,-74.13 5.28,-79.47 3.9,-84.95 C3.45,-86.74 1.85,-88 0,-88 C-1.85,-88 -3.45,-86.74 -3.9,-84.95 Z"
      transform="translate(64 64) scale(0.4)"
    />
    <g class="orbit-dots orbit-dots-comet">
      <circle
        class="orbit-dot orbit-dot-faint"
        cx="64"
        cy="24"
        r="4.1"
      />
    </g>
    <g class="orbit-dots orbit-dots-comet-reverse">
      <circle
        class="orbit-dot orbit-dot-secondary"
        cx="68"
        cy="108"
        r="4.9"
      />
      <circle
        class="orbit-dot orbit-dot-primary"
        cx="60"
        cy="113"
        r="7.4"
      />
    </g>
  </svg>
</template>

<script setup lang="ts">
  interface Props {
    /** The visual variant to render. Defaults to `'star'`. */
    variant?: 'star' | 'star-orbit-comet-5' | 'star-orbit-comet-5-reverse' | 'star-error'
  }

  withDefaults(defineProps<Props>(), {
    variant: 'star',
  })
</script>

<style lang="scss" scoped>
  .agent-working-indicator {
    width: 35px;
    height: 35px;
    display: block;
    flex: 0 0 auto;
    overflow: visible;
  }

  .star-core {
    opacity: 0.94;
    transform-origin: center;
    transform-box: fill-box;
  }

  .orbit-dots {
    transform-origin: 64px 64px;
    animation: orbitSpin 1.9s infinite linear;
  }

  .orbit-dots-comet {
    animation-duration: 1.7s;
  }

  .orbit-dots-comet-reverse {
    animation-duration: 2.5s;
    animation-direction: reverse;
  }

  .orbit-dot {
    fill: #ed1c24;
  }

  .orbit-dot-primary {
    opacity: 0.96;
  }

  .orbit-dot-secondary {
    opacity: 0.72;
  }

  .orbit-dot-faint {
    opacity: 0.44;
  }

  @keyframes orbitSpin {
    from {
      transform: rotate(0deg);
    }

    to {
      transform: rotate(360deg);
    }
  }
</style>
