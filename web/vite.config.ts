import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Allow `VITE_API_TARGET` (e.g. http://127.0.0.1:8080 or http://192.168.100.10:8080)
  // to override the default. Without it `npm run dev` would serve `/api/*`
  // from the Vite dev server itself (returning index.html), which silently
  // breaks every API call. The fallback assumes you're running the center
  // on the same machine as the dev server.
  const env = loadEnv(mode, process.cwd(), '')
  const target = env.VITE_API_TARGET ?? 'http://127.0.0.1:8080'

  return {
    plugins: [react()],
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
