// @ts-check
import react from '@astrojs/react'
import sitemap from '@astrojs/sitemap'
import starlight from '@astrojs/starlight'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'astro/config'
import nativeParser from './src/components/docs/native-parser.mjs'

import docsSidebar from './src/docs-sidebar.mjs'

// Public site: the landing page lives at `/` (src/pages) and the
// documentation at `/docs` (src/content/docs/docs), rendered by Starlight.
// English is the default locale; Spanish pages live under `/es`.
export default defineConfig({
  site: 'https://excalibur.crehlabs.com',
  integrations: [
    starlight({
      title: 'Excalibur',
      plugins: [nativeParser],
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
      customCss: ['./src/styles/fonts.css', './src/styles/global.css', './src/styles/docs.css'],
      sidebar: docsSidebar,
      components: {
        Head: './src/components/docs/Head.astro',
        Header: './src/components/docs/Header.astro',
        MobileMenuFooter: './src/components/docs/MobileMenuFooter.astro',
        ThemeProvider: './src/components/docs/ThemeProvider.astro',
        ThemeSelect: './src/components/docs/ThemeSelect.astro',
        TableOfContents: './src/components/docs/TableOfContents.astro',
        PageTitle: './src/components/docs/PageTitle.astro',
        Hero: './src/components/docs/Hero.astro',
      },
      expressiveCode: {
        themes: ['github-light', 'github-dark'],
        themeCssSelector: (theme) => `[data-theme='${theme.type}']`,
        useDarkModeMediaQuery: false,
        styleOverrides: {
          borderRadius: '0',
          borderWidth: '1px',
          uiFontFamily: 'var(--font-mono)',
          uiFontSize: '0.75rem',
          borderColor: 'var(--hairline)',
          codeBackground: 'var(--surface)',
          codeForeground: 'var(--ink)',
          codeFontFamily: 'var(--font-mono)',
          codeFontSize: '0.8125rem',
          codeLineHeight: '1.7',
          frames: {
            frameBoxShadowCssValue: 'none',
            editorActiveTabBackground: 'var(--bg)',
            editorActiveTabForeground: 'var(--ink)',
            editorActiveTabIndicatorTopColor: 'var(--blue)',
            editorActiveTabIndicatorBottomColor: 'var(--hairline)',
            editorTabBarBackground: 'var(--surface)',
            editorTabBarBorderColor: 'var(--hairline)',
            editorTabBarBorderBottomColor: 'var(--hairline)',
            terminalTitlebarBackground: 'var(--surface)',
            terminalTitlebarForeground: 'var(--ink-2)',
            terminalTitlebarBorderBottomColor: 'var(--hairline)',
            terminalTitlebarDotsOpacity: '0',
            terminalBackground: 'var(--surface)',
          },
          textMarkers: {
            markBackground: 'var(--blue-tint)',
            markBorderColor: 'var(--blue)',
            insBackground: 'var(--bg)',
            insBorderColor: 'var(--diff-add)',
            insDiffIndicatorColor: 'var(--diff-add)',
            delBackground: 'var(--bg)',
            delBorderColor: 'var(--diff-del)',
            delDiffIndicatorColor: 'var(--diff-del)',
          },
        },
      },
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
