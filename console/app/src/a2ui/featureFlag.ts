/**
 * Product feature flag for A2UI support on the ai.v1 agent.
 *
 * Set to `true` when the agent can emit and handle A2UI surfaces over AG-UI.
 * Until then the settings toggle stays visible but disabled, and the console
 * does not forward `a2uiSchema` in `runAgent` forwardedProps.
 */
export const A2UI_AGENT_SUPPORTED = false
