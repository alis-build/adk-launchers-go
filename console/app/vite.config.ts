// Plugins
import Vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import vueDevTools from 'vite-plugin-vue-devtools'
import Vuetify, { transformAssetUrls } from 'vite-plugin-vuetify'

// Utilities
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

const frontendAppPort = Number.parseInt(process.env.FRONTEND_APP_PORT ?? '8000', 10)

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    Components({
      dts: true,
      deep: true,
      globs: ['src/components/**/*.vue', 'src/pages/**/components/**/*.vue'],
      types: [
        {
          from: 'vue-router',
          names: ['RouterLink', 'RouterView'],
        },
      ],
    }),
    Vue({
      template: { transformAssetUrls },
    }),
    // https://github.com/vuetifyjs/vuetify-loader/tree/master/packages/vite-plugin#readme
    Vuetify({
      autoImport: true,
      styles: {
        configFile: 'src/plugins/vuetify/vuetify.scss',
      },
    }),
    vueDevTools(),
  ],
  optimizeDeps: {
    exclude: ['vuetify', 'vue-router'],
  },
  define: { 'process.env': {} },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
    extensions: ['.js', '.json', '.jsx', '.mjs', '.ts', '.tsx', '.vue'],
  },
  build: {
    rollupOptions: {
      onwarn(warning, warn) {
        if (warning.code === 'EVAL' && warning.id?.includes('google-protobuf')) {
          return
        }
        warn(warning)
      },
    },
  },
  server: {
    port: Number.isNaN(frontendAppPort) ? 8000 : frontendAppPort,
    allowedHosts: true,
    proxy: {
      '/agui': { target: process.env.AGENT_HOST ?? 'http://localhost:8080', changeOrigin: true },
      '/auth': { target: process.env.AGENT_HOST ?? 'http://localhost:8080', changeOrigin: true },
      '/alis.agui.history.v1.ThreadService': {
        target: process.env.AGENT_HOST ?? 'http://localhost:8080',
        changeOrigin: true,
      },
      '/alis.a2a.extension.v1.SchedulerService': {
        target: process.env.AGENT_HOST ?? 'http://localhost:8080',
        changeOrigin: true,
      },
      '/assets/config/runtime-config.json': {
        target: process.env.AGENT_HOST ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
