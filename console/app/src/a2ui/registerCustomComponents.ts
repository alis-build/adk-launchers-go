/**
 * Registers application-specific A2UI custom components with the
 * Vuetify renderer's component registry.
 *
 * Custom components extend the base Vuetify catalog with domain-specific
 * widgets that agents can reference in A2UI surface definitions.
 * Registration happens at module load time (before any MessageProcessor
 * processes envelopes) so that components are available when replaying
 * history as well as during live streaming.
 *
 * @module a2ui/registerCustomComponents
 */
import { CATALOG_ID, defaultRegistry } from '@alis-build/a2ui-vuetify-renderer'
import CapabilitiesCard, { CapabilitiesCardApi } from './components/CapabilitiesCard.vue'

/**
 * Registers all custom A2UI components into the default renderer registry.
 *
 * Currently registers:
 * - `CapabilitiesCard` - Displays agent capability information as a card.
 *
 * Called once at module load time by `processor.ts`.
 */
export function registerCustomComponents() {
  defaultRegistry.register(CATALOG_ID, 'CapabilitiesCard', CapabilitiesCard, CapabilitiesCardApi)
}
