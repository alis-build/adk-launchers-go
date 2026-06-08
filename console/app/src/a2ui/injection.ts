/**
 * Vue injection key for the A2UI action sender.
 *
 * The host view `provide`s an implementation that sends A2UI client actions
 * to the agent. The {@link A2UISurface} component `inject`s it so user
 * interactions with rendered surfaces flow back to the agent.
 *
 * @module a2ui/injection
 */
import type { A2uiClientAction } from '@a2ui/web_core/v0_9'
import type { InjectionKey } from 'vue'

/** Signature of the function that sends an A2UI client action to the agent. */
export type A2UISendActionFn = (action: A2uiClientAction) => Promise<void>

/** Vue injection key — `provide` in the host view, `inject` in A2UISurface. */
export const a2uiSendActionKey: InjectionKey<A2UISendActionFn> = Symbol('a2uiSendAction')
