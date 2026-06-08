/**
 * Vite environment type declarations.
 *
 * Augments the global `ImportMeta` interface so that `import.meta.env.*`
 * values are typed throughout the application. Also stubs the Vuetify
 * styles module to satisfy TypeScript when importing Vuetify CSS.
 *
 * @module vite-env
 */

/** Stub module declaration so `import 'vuetify/styles'` does not error. */
declare module 'vuetify/styles' {}

/** Placeholder for Vite's type options (currently unused). */
interface ViteTypeOptions {}

/** Build-time environment variables injected by Vite. */
interface ImportMetaEnv {
  /** The base URL path the app is served from (set in `vite.config.ts`). */
  BASE_URL: string
}

/** Extends the standard `ImportMeta` with Vite's typed env. */
interface ImportMeta {
  readonly env: ImportMetaEnv
}
