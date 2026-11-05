/** Public documentation navigation. Leaf titles come from each locale's frontmatter. */
export const docsSidebar = [
  {
    label: 'Getting started',
    translations: { es: 'Primeros pasos' },
    items: [
      'docs/getting-started/what-is-excalibur',
      'docs/getting-started/install',
      'docs/getting-started/quick-start',
      'docs/getting-started/providers',
    ],
  },
  {
    label: 'Everyday use',
    translations: { es: 'Uso diario' },
    items: [
      'docs/everyday/chat-voice',
      'docs/everyday/browser-passwords',
      'docs/everyday/memory',
      'docs/everyday/documents',
      'docs/everyday/google',
      'docs/everyday/scheduled-tasks',
    ],
  },
  {
    label: 'Data',
    translations: { es: 'Datos' },
    items: ['docs/data/my-data', 'docs/data/import-spreadsheet', 'docs/data/interfaces'],
  },
  {
    label: 'Building software',
    translations: { es: 'Crear software' },
    items: ['docs/building/agents', 'docs/building/coding', 'docs/building/public-preview'],
  },
  {
    label: 'Extensions',
    translations: { es: 'Extensiones' },
    items: [
      'docs/extensions/overview',
      'docs/extensions/classroom',
      'docs/extensions/robotics-lab',
    ],
  },
  {
    label: 'Reference',
    translations: { es: 'Referencia' },
    items: ['docs/reference/cli', 'docs/reference/configuration', 'docs/reference/security'],
  },
]

export default docsSidebar
