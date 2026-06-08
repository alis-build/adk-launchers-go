/**
 * Pinia store for the user-facing A2UI feature toggle (default **off**, opt-in).
 *
 * - **Persistence:** `localStorage` key `a2ui-enabled`; only the string `'true'` is treated as
 *   enabled. Missing or any other value means off until the user turns A2UI on in settings.
 * - **Feature flag:** {@link A2UI_AGENT_SUPPORTED} — when false, toggle is disabled and
 *   {@link effectiveA2uiEnabled} is always false.
 * - **Consumers:** `useChatStream` uses `effectiveA2uiEnabled` for AG-UI `forwardedProps`.
 *   `AppSettingsMenu` binds the switch to `a2uiEnabled`.
 *
 * @module a2ui/settings
 */
import { A2UI_AGENT_SUPPORTED } from '@/a2ui/featureFlag'
import { defineStore } from 'pinia'
import { computed, type ComputedRef, type Ref } from 'vue'
import { ref, watch } from 'vue'

/** Storage key shared with any code that might read the preference outside Pinia (keep in sync). */
const STORAGE_KEY = 'a2ui-enabled'

/** Read the persisted value from localStorage (defaults to `false` so A2UI is opt-in). */
function readPersistedValue(): boolean {
  if (!A2UI_AGENT_SUPPORTED) {
    return false
  }
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    return stored === 'true'
  } catch {
    return false
  }
}

/** Pinia store for the A2UI user toggle and {@link effectiveA2uiEnabled}. */
export const useA2UISettingsStore = defineStore('a2uiSettings', () => {
  /**
   * User preference from settings (only meaningful when {@link A2UI_AGENT_SUPPORTED}).
   */
  const a2uiEnabled: Ref<boolean> = ref(readPersistedValue())

  /** Whether the agent product supports A2UI (feature flag). */
  const a2uiAgentSupported = A2UI_AGENT_SUPPORTED

  /** Settings switch is non-interactive when the agent does not support A2UI yet. */
  const a2uiToggleDisabled = !A2UI_AGENT_SUPPORTED

  /**
   * True when A2UI should be active: agent supports it and the user opted in.
   * Use this for `forwardedProps`, surface replay, and action dispatch.
   */
  const effectiveA2uiEnabled: ComputedRef<boolean> = computed(
    () => A2UI_AGENT_SUPPORTED && a2uiEnabled.value,
  )

  watch(a2uiEnabled, (val) => {
    if (!A2UI_AGENT_SUPPORTED) {
      if (a2uiEnabled.value) {
        a2uiEnabled.value = false
      }
      return
    }
    try {
      localStorage.setItem(STORAGE_KEY, String(val))
    } catch {
      // Private mode or quota: preference applies for this session only.
    }
  })

  /** Flips `a2uiEnabled` (e.g. from the settings menu switch). */
  function toggleA2UI() {
    if (!A2UI_AGENT_SUPPORTED) return
    a2uiEnabled.value = !a2uiEnabled.value
  }

  return {
    a2uiEnabled,
    a2uiAgentSupported,
    a2uiToggleDisabled,
    effectiveA2uiEnabled,
    toggleA2UI,
  }
})
