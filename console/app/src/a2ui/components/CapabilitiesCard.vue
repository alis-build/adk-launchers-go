<script setup lang="ts">
  import type { ComponentApi, ComponentModel } from '@a2ui/web_core/v0_9'
  import { useA2UI } from '@alis-build/a2ui-vuetify-renderer'
  import { computed } from 'vue'

  const props = defineProps<{
    /** The A2UI component model node containing the capabilities data. */
    node: ComponentModel
  }>()

  const { resolveValue } = useA2UI()

  /** A single agent capability with a title and description. */
  interface Capability {
    title: string
    description: string
  }

  /** Resolved list of capabilities from the A2UI data model. */
  const capabilities = computed(() => {
    const data = resolveValue<Capability[]>(props.node?.properties.capabilities)
    return data as Capability[]
  })
</script>

<script lang="ts">
  import { z } from 'zod'

  export const CapabilitiesCardApi: ComponentApi = {
    name: 'CapabilitiesCard',
    schema: z
      .object({
        capabilities: z
          .array(
            z
              .object({
                title: z.string().describe('The title of the capability'),
                description: z.string().describe('The description of the capability'),
              })
              .strict(),
          )
          .describe('The capabilities of the agent'),
      })
      .strict(),
  }
</script>

<template>
  <v-card
    elevation="0"
    color="primary"
    variant="tonal"
  >
    <template #prepend>
      <v-avatar color="primary">
        <v-icon>bolt</v-icon>
      </v-avatar>
    </template>
    <template v-slot:title>
      <span class="font-weight-black">Capabilities</span>
    </template>

    <v-card-text>
      <v-list>
        <v-list-item
          v-for="capability in capabilities"
          :key="capability.title"
        >
          <v-list-item-title> {{ capability.title }} </v-list-item-title>
          <v-list-item-subtitle> {{ capability.description }} </v-list-item-subtitle>
        </v-list-item>
      </v-list>
    </v-card-text>
  </v-card>
</template>

<style lang="scss"></style>
