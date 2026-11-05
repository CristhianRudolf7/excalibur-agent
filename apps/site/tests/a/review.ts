import sharp from 'sharp'
import { mkdir } from 'node:fs/promises'
const out = '/home/cristhian/projects/ui-conceptos/landing/implementacion/a'
await mkdir(`${out}/review`, { recursive: true })
const names = ['your-day', 'memory', 'data']
for (const width of [1024, 1280, 1536, 1920]) {
  const tiles = await Promise.all(
    ['light', 'dark'].flatMap((theme, row) =>
      names.map(async (name, col) => ({
        input: await sharp(`${out}/${name}-${width}-${theme}.png`)
          .resize(512, 341)
          .png()
          .toBuffer(),
        left: col * 512,
        top: row * 341,
      })),
    ),
  )
  await sharp({ create: { width: 1536, height: 682, channels: 3, background: '#ffffff' } })
    .composite(tiles)
    .png()
    .toFile(`${out}/review/all-${width}.png`)
}
for (const [index, name] of names.entries()) {
  const concept = `/home/cristhian/projects/ui-conceptos/landing/resto/seccion-${index + 1}-${name}.png`
  const tiles = await Promise.all(
    [concept, `${out}/${name}-1536-light.png`].map(async (path, col) => ({
      input: await sharp(path).resize(768, 512).png().toBuffer(),
      left: col * 768,
      top: 0,
    })),
  )
  await sharp({ create: { width: 1536, height: 512, channels: 3, background: '#ffffff' } })
    .composite(tiles)
    .png()
    .toFile(`${out}/review/${name}-concept-comparison.png`)
}
