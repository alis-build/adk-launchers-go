<script setup lang="ts">
  import { useAppPanelCardHeight } from '@/utils/sizing'
  import { onMounted } from 'vue'

  const { appPanelCardRef, appPanelCardHeight } = useAppPanelCardHeight()

  onMounted(() => {
    void appPanelCardRef.value
  })
</script>

<template>
  <div
    :class="['h-100', 'd-flex', 'justify-center', 'thread-shell']"
    class="px-2 px-md-4"
  >
    <div
      ref="appPanelCardRef"
      :style="{ minHeight: `${appPanelCardHeight}px`, height: `${appPanelCardHeight}px` }"
      class="thread-main d-flex flex-column flex-grow-1 min-w-0"
    >
      <div class="thread-main-content d-flex flex-column flex-grow-1 min-h-0">
        <div class="thread-main-route flex-grow-1 min-h-0">
          <router-view v-slot="{ Component }">
            <component
              :is="Component"
              class="thread-route-component"
            />
          </router-view>
        </div>
      </div>
    </div>
  </div>
</template>

<style lang="scss" scoped>
  .thread-shell {
    min-height: 0;
    width: 100%;
  }

  .thread-main {
    max-width: 1120px;
    width: 100%;
  }

  .thread-main-content {
    min-height: 0;
    overflow: hidden;
  }

  .thread-main-route {
    height: 100%;
    min-height: 0;
  }

  .thread-route-component {
    height: 100%;
    min-height: 0;
    display: block;
  }
</style>
