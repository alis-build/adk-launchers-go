/**
 * Deploy-time runtime configuration loaded from the console backend.
 *
 * @module runtimeConfig
 */
import { readonly, ref, type DeepReadonly, type Ref } from 'vue'

export interface RuntimeBranding {
  title?: string
  displayName?: string
  faviconUrl?: string
  logoUrl?: string
}

/**
 * Shape of `/assets/config/runtime-config.json` served by the console launcher.
 */
export interface RuntimeConfig {
  gcpProject?: string
  agents?: string[]
  defaultAgentId?: string
  branding?: RuntimeBranding
}

const runtimeConfig: Ref<RuntimeConfig> = ref({})

/** Reactive runtime config for computed properties in components and stores. */
export const runtimeConfigState = readonly(runtimeConfig) as DeepReadonly<Ref<RuntimeConfig>>

/**
 * Loads deploy-time config served by the console launcher.
 */
export async function loadRuntimeConfig(): Promise<RuntimeConfig> {
  try {
    const response = await fetch('/assets/config/runtime-config.json', { cache: 'no-store' })
    if (!response.ok) return runtimeConfig.value
    const data = (await response.json()) as Partial<RuntimeConfig>
    runtimeConfig.value = {
      gcpProject: typeof data.gcpProject === 'string' ? data.gcpProject : undefined,
      agents: Array.isArray(data.agents) ? data.agents.filter((a): a is string => typeof a === 'string') : undefined,
      defaultAgentId: typeof data.defaultAgentId === 'string' ? data.defaultAgentId : undefined,
      branding: data.branding && typeof data.branding === 'object' ? data.branding : undefined,
    }
  } catch {
    // Leave defaults when runtime config cannot be loaded.
  }
  return runtimeConfig.value
}

/** Returns the current runtime configuration snapshot. */
export function getRuntimeConfig(): RuntimeConfig {
  return runtimeConfig.value
}

/** Default agent id from runtime config, or first listed agent. */
export function getDefaultAgentId(): string {
  const cfg = runtimeConfig.value
  if (cfg.defaultAgentId) return cfg.defaultAgentId
  return cfg.agents?.[0] ?? ''
}

/** Shell title from branding or a neutral default (applied before runtime config loads). */
export function getShellTitle(): string {
  const branding = runtimeConfig.value.branding
  return branding?.displayName || branding?.title || 'Agent Console'
}

/** Logo URL from branding with a bundled fallback. */
export function getShellLogoUrl(): string {
  return runtimeConfig.value.branding?.logoUrl || '/logo.svg'
}
