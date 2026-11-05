import { cp, mkdir, rm, access } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
const root = new URL('../../', import.meta.url)
const source = new URL('apps/web/dist/', root)
const target = new URL('internal/webui/assets/built/', root)
await access(new URL('index.html', source))
await rm(target, { recursive: true, force: true })
await mkdir(target, { recursive: true })
await cp(source, target, { recursive: true })
console.log(`Embedded web assets prepared at ${fileURLToPath(target)}`)
