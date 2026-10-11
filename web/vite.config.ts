import tailwindcss from '@tailwindcss/vite'
import type { Plugin } from 'vite'
import { defineConfig } from 'vitest/config'

import { applyPolicy } from './src/lib/csp'

// contentSecurityPolicy writes the policy into the built page, using the API address that this build was given.
function contentSecurityPolicy(): Plugin {
  let apiUrl: string | undefined
  return {
    name: 'shadow-pager:content-security-policy',
    // The dev server needs inline scripts for hot reloading, so the policy is only written for real builds.
    apply: 'build',
    configResolved(config) {
      const value: unknown = config.env['VITE_API_URL']
      apiUrl = typeof value === 'string' ? value : undefined
    },
    transformIndexHtml: {
      // Running last means the hashes are taken from the page exactly as it will be served.
      order: 'post',
      handler: (html) => applyPolicy(html, apiUrl),
    },
  }
}

export default defineConfig({
  plugins: [tailwindcss(), contentSecurityPolicy()],
  server: {
    port: 5173,
    // In local development the browser talks to this server only, and it passes API calls on to the Go server.
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
  },
})
