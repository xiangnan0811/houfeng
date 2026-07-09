import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import { visualizer } from 'rollup-plugin-visualizer'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Allow `VITE_API_TARGET` (e.g. http://127.0.0.1:8080 or http://192.168.100.10:8080)
  // to override the default. Without it `npm run dev` would serve `/api/*`
  // from the Vite dev server itself (returning index.html), which silently
  // breaks every API call. The fallback assumes you're running the center
  // on the same machine as the dev server.
  const env = loadEnv(mode, process.cwd(), '')
  const target = env.VITE_API_TARGET ?? 'http://127.0.0.1:8080'

  // CSS/JS bundle analysis is opt-in via `ANALYZE=1 npm run build`.
  // Gated so normal dev/build is untouched and the dev server never loads it.
  const plugins = [react()]
  if (process.env.ANALYZE) {
    plugins.push(
      visualizer({
        filename: './dist/stats.html',
        gzipSize: true,
        brotliSize: true,
        template: 'treemap',
      }),
    )
  }

  return {
    plugins,
    server: {
      proxy: {
        '/api': {
          target,
          changeOrigin: true,
        },
      },
    },
  }
})
