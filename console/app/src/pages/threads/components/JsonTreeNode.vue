<template>
  <div class="json-node">
    <div
      v-if="isExpandable"
      class="json-node__row"
    >
      <v-btn
        icon
        size="x-small"
        variant="text"
        density="compact"
        class="json-node__toggle"
        @click="expanded = !expanded"
      >
        <v-icon size="16">{{ expanded ? 'keyboard_arrow_down' : 'keyboard_arrow_right' }}</v-icon>
      </v-btn>

      <span
        v-if="label"
        class="json-node__key"
      >{{ label }}:</span>
      <span class="json-node__punctuation">{{ openingToken }}</span>
      <span
        v-if="!expanded"
        class="json-node__summary"
      >{{ collapsedSummary }}</span>
      <span class="json-node__punctuation">{{ closingToken }}</span>
    </div>

    <div
      v-else
      class="json-node__row json-node__row--leaf"
    >
      <span
        v-if="label"
        class="json-node__key"
      >{{ label }}:</span>
      <span
        class="json-node__value"
        :class="valueClass"
      >{{ formattedValue }}</span>
    </div>

    <div
      v-if="isExpandable && expanded"
      class="json-node__children"
    >
      <JsonTreeNode
        v-for="child in childEntries"
        :key="child.key"
        :label="child.label"
        :value="child.value"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, ref } from 'vue'

  interface Props {
    /** Optional label for this node (object key or array index). */
    label?: string
    /** The JSON value to render (object, array, or primitive). */
    value: unknown
  }

  const props = defineProps<Props>()

  const expanded = ref(true)

  const isArray = computed(() => Array.isArray(props.value))
  const isObject = computed(() => props.value !== null && typeof props.value === 'object' && !Array.isArray(props.value))
  const isExpandable = computed(() => isArray.value || isObject.value)

  const childEntries = computed(() => {
    if (isArray.value) {
      return (props.value as unknown[]).map((value, index) => ({
        key: `[${index}]`,
        label: `[${index}]`,
        value,
      }))
    }
    if (isObject.value) {
      return Object.entries(props.value as Record<string, unknown>).map(([key, value]) => ({
        key,
        label: key,
        value,
      }))
    }
    return []
  })

  const openingToken = computed(() => (isArray.value ? '[' : '{'))
  const closingToken = computed(() => (isArray.value ? ']' : '}'))

  const collapsedSummary = computed(() => {
    if (isArray.value) {
      const length = (props.value as unknown[]).length
      return length === 0 ? '' : ` ${length} item${length === 1 ? '' : 's'} `
    }
    const length = Object.keys(props.value as Record<string, unknown>).length
    return length === 0 ? '' : ` ${length} key${length === 1 ? '' : 's'} `
  })

  const formattedValue = computed(() => {
    if (typeof props.value === 'string') return `"${props.value}"`
    if (props.value === null) return 'null'
    return String(props.value)
  })

  const valueClass = computed(() => {
    if (props.value === null) return 'json-node__value--null'
    switch (typeof props.value) {
      case 'string':
        return 'json-node__value--string'
      case 'number':
        return 'json-node__value--number'
      case 'boolean':
        return 'json-node__value--boolean'
      default:
        return 'json-node__value--plain'
    }
  })
</script>

<style scoped lang="scss">
  .json-node {
    font-family: monospace;
    font-size: 12px;
    line-height: 1.5;
  }

  .json-node__row {
    display: flex;
    align-items: flex-start;
    gap: 4px;
    min-height: 22px;
  }

  .json-node__row--leaf {
    padding-left: 28px;
  }

  .json-node__toggle {
    margin-top: -2px;
    flex: 0 0 auto;
  }

  .json-node__children {
    padding-left: 20px;
    border-left: 1px solid rgba(var(--v-theme-on-surface), 0.08);
    margin-left: 12px;
  }

  .json-node__key {
    color: rgb(var(--v-theme-primary));
    word-break: break-word;
  }

  .json-node__punctuation,
  .json-node__summary {
    color: rgba(var(--v-theme-on-surface), 0.65);
  }

  .json-node__value {
    word-break: break-word;
  }

  .json-node__value--string {
    color: rgb(var(--v-theme-success));
  }

  .json-node__value--number {
    color: rgb(var(--v-theme-info));
  }

  .json-node__value--boolean {
    color: rgb(var(--v-theme-warning));
  }

  .json-node__value--null {
    color: rgba(var(--v-theme-on-surface), 0.55);
    font-style: italic;
  }
</style>
