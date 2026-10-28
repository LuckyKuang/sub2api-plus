import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// An independent test entrypoint: no production router, backend, proxy, public
// configuration requests, or changes to the production build inputs.
export default defineConfig({
  root: frontend,
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(frontend, 'src'),
      'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js',
    },
  },
  define: { __INTLIFY_JIT_COMPILATION__: true },
  server: { host: '127.0.0.1', port: 18193, strictPort: true },
})
