/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

// The daemon gateway serves JSON-RPC on /rpc, events on /ws and blobs on
// /blobs. In development Vite proxies those paths to a locally running
// daemon (`excalibur daemon --listen 127.0.0.1:7787`).
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const daemon = env.EXCALIBUR_DAEMON_URL || 'http://127.0.0.1:7787'

  return {
    define: {
      'import.meta.env.EXCALIBUR_WEB_ROBOTLAB': JSON.stringify(env.EXCALIBUR_WEB_ROBOTLAB ?? '1'),
      'import.meta.env.EXCALIBUR_WEB_WORK_EVENTS': JSON.stringify(
        env.EXCALIBUR_WEB_WORK_EVENTS ?? '1',
      ),
      'import.meta.env.EXCALIBUR_WEB_WHITEBOARD': JSON.stringify(
        env.EXCALIBUR_WEB_WHITEBOARD ?? '1',
      ),
      'import.meta.env.EXCALIBUR_WEB_CHAT_LIVE': JSON.stringify(env.EXCALIBUR_WEB_CHAT_LIVE ?? '1'),
      'import.meta.env.EXCALIBUR_WEB_LIBRARY': JSON.stringify(env.EXCALIBUR_WEB_LIBRARY || '1'),
      'import.meta.env.EXCALIBUR_DOCUMENT_VIEWER': JSON.stringify(
        env.EXCALIBUR_DOCUMENT_VIEWER ?? '1',
      ),
    },
    plugins: [
      ...(mode === 'test'
        ? [
            {
              name: 'robotlab-viewer-test-contract',
              enforce: 'pre' as const,
              resolveId(source: string, importer?: string) {
                if (source === './viewer' && importer?.includes('/features/robotlab/'))
                  return fileURLToPath(
                    new URL('./e2e/fixtures/robotlab-viewer.tsx', import.meta.url),
                  )
              },
            },
          ]
        : []),
      tanstackRouter({ target: 'react', autoCodeSplitting: true }),
      react(),
      tailwindcss(),
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      port: 5173,
      proxy: {
        '/rpc': daemon,
        '/blobs': daemon,
        '/healthz': daemon,
        '/ws': { target: daemon, ws: true },
      },
    },
    test: {
      environment: 'jsdom',
      testTimeout: Number(process.env.EXCALIBUR_TEST_TIMEOUT) || 20_000,
      maxWorkers: Number(process.env.EXCALIBUR_TEST_WORKERS) || 2,
      globals: true,
      setupFiles: ['./src/test/setup.ts'],
      include: ['src/**/*.test.{ts,tsx}'],
    },
  }
})
