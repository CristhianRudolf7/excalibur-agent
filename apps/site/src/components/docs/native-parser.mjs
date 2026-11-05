// Starlight 0.42's Steps/Tabs use satteri. Keep the native parser in its
// installed package so its platform binding resolves during static rendering.
import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const starlightRequire = createRequire(
  require.resolve('@astrojs/starlight/components/Sidebar.astro'),
)
const parser = starlightRequire.resolve('satteri')

/** @type {import('@astrojs/starlight/types').StarlightPlugin} */
export default {
  name: 'manual-native-parser',
  hooks: {
    'config:setup'({ addIntegration }) {
      addIntegration({
        name: 'manual-native-parser-build',
        hooks: {
          'astro:config:setup'({ updateConfig }) {
            updateConfig({
              vite: {
                plugins: [
                  {
                    name: 'manual-native-parser-external',
                    enforce: 'pre',
                    resolveId(id, _importer, options) {
                      if (id === 'satteri' && options?.ssr) return { id: parser, external: true }
                    },
                  },
                ],
              },
            })
          },
        },
      })
    },
  },
}
