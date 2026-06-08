/**
 * Selected ADK agent for multi-agent hosts.
 *
 * @module store/selectedAgent
 */
import { getDefaultAgentId, getRuntimeConfig, getShellTitle, runtimeConfigState } from '@/runtimeConfig'
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

const STORE_ID = 'selectedAgent'

export const useSelectedAgentStore = defineStore(STORE_ID, () => {
  const selectedAgentId = ref(getDefaultAgentId())

  const agents = computed(() => runtimeConfigState.value.agents ?? [])

  const showAgentSelector = computed(() => agents.value.length > 0)

  const shellTitle = computed(() => {
    const branding = runtimeConfigState.value.branding
    if (branding?.displayName) return branding.displayName
    if (branding?.title) return branding.title
    if (selectedAgentId.value) return selectedAgentId.value
    return getShellTitle()
  })

  function setSelectedAgentId(agentId: string) {
    selectedAgentId.value = agentId
  }

  function ensureDefaultAgent() {
    const cfg = getRuntimeConfig()
    const list = cfg.agents ?? []
    if (list.length === 0) {
      selectedAgentId.value = cfg.defaultAgentId ?? ''
      return
    }
    if (!list.includes(selectedAgentId.value)) {
      selectedAgentId.value = cfg.defaultAgentId && list.includes(cfg.defaultAgentId) ? cfg.defaultAgentId : list[0]
    }
  }

  return {
    selectedAgentId,
    agents,
    showAgentSelector,
    shellTitle,
    setSelectedAgentId,
    ensureDefaultAgent,
  }
})
