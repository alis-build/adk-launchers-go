/**
 * A2UI integration module — self-contained and portable.
 *
 * This directory contains all A2UI-specific logic: settings persistence,
 * MessageProcessor lifecycle, action building, detection helpers, and the
 * Vue injection key for action dispatching.
 *
 * To port A2UI to another project, copy this directory and install the
 * required peer dependencies:
 * - `@a2ui/web_core`
 * - `@alis-build/a2ui-vuetify-renderer`
 * - `@alis-build/common-es`
 * - `@bufbuild/protobuf`
 * - `pinia`
 * - `vue`
 *
 * @module a2ui
 */
export { A2UI_AGENT_SUPPORTED } from './featureFlag'
export { buildA2UIActionPart } from './actionBuilder'
export { getA2UICreateSurfaceId, isA2UIMessage, shouldRenderA2UIDataPartInChat } from './detection'
export { a2uiSendActionKey, type A2UISendActionFn } from './injection'
export {
  getOrCreateA2uiProcessor,
  ingestA2uiPayloadOnce,
  normalizeToA2uiMessage,
  resetA2uiProcessorScope,
  setA2uiActionHandler,
} from './processor'
export { useA2UISettingsStore } from './settings'
