import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// The same production CSS pipeline as the app, with an isolated real-component
// entrypoint. Runtime Vue transition classes must survive Tailwind generation.
export default defineConfig({
  root: frontend,
  plugins: [vue()],
  resolve: { alias: {
    '@': resolve(frontend, 'src'),
    'vue-i18n': 'vue-i18n/dist/vue-i18n.mjs',
  } },
  build: {
    outDir: process.env.SUB2API_MOTION_DIST || '/tmp/sub2api-motion-dist',
    emptyOutDir: true,
    rollupOptions: { input: resolve(frontend, 'layout-tests/motion.html') },
  },
  preview: { host: '127.0.0.1', port: 18194, strictPort: true },
})
