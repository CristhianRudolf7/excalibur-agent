// @ts-check
import react from '@astrojs/react'
import sitemap from '@astrojs/sitemap'
import starlight from '@astrojs/starlight'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'astro/config'

// Public site: the landing page lives at `/` (src/pages) and the
// documentation at `/docs` (src/content/docs/docs), rendered by Starlight.
// English is the default locale; Spanish pages live under `/es`.
export default defineConfig({
  // Replace with the production domain before the first deploy.
  site: 'https://excalibur.example.com',
  integrations: [
    starlight({
      title: 'Excalibur',
      defaultLocale: 'root',
      locales: {
        root: { label: 'English', lang: 'en' },
        es: { label: 'Español', lang: 'es' },
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/CristhianRudolf7/excalibur-agent',
        },
      ],
      customCss: ['./src/styles/fonts.css', './src/styles/global.css'],
      sidebar: [{ label: 'Documentation', items: [{ autogenerate: { directory: 'docs' } }] }],
    }),
    react(),
    sitemap(),
  ],
  vite: {
    plugins: [
      tailwindcss(),
      {
        name: 'retain-gsap-license',
        enforce: 'post',
        generateBundle(_options, bundle) {
          for (const asset of Object.values(bundle)) {
            if (
              asset.type === 'chunk' &&
              Object.keys(asset.modules).some((id) => id.includes('/gsap/'))
            ) {
              asset.code =
                '/*! GSAP & ScrollTrigger 3.15.0 | Copyright 2008-2026 GreenSock. All rights reserved. | @license https://gsap.com/standard-license | Author: Jack Doyle, jack@greensock.com */\n' +
                asset.code
            }
          }
        },
      },
    ],
  },
})
