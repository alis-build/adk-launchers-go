/**
 * Pinia state management plugin instance.
 *
 * Creates and exports a single Pinia instance used by all stores
 * in the application (app, snackbar, threadComposer, threads, etc.).
 *
 * @module plugins/pinia
 */
import { createPinia } from 'pinia'

export default createPinia()
