import { defineCollection } from 'astro:content'
import { i18nLoader, docsLoader } from '@astrojs/starlight/loaders'
import { i18nSchema, docsSchema } from '@astrojs/starlight/schema'

export const collections = {
  i18n: defineCollection({ loader: i18nLoader(), schema: i18nSchema() }),
  docs: defineCollection({ loader: docsLoader(), schema: docsSchema() }),
}
