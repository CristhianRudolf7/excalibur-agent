import type { FileChange } from '@/lib/rpc/contract'
export interface ProjectFile {
  path: string
  content: string
  changed?: boolean
}
const files: ProjectFile[] = [
  {
    path: 'results/voice.md',
    content:
      '# Voice model dossier\n\nMock research report.\n\nThree voice models are being compared for latency, quality, and streaming support. Excalibur gathers sources and Excalibur runs the simulated latency checks.\n',
  },
  {
    path: 'results/library.md',
    content:
      '# Research sources\n\nEight reference sources collected in the mock research mission. Review provider documentation before choosing a voice model.\n',
  },
  {
    path: 'results/design.md',
    content:
      '# Comparison design\n\nMock deliverable: a visual comparison layout with model, latency, quality, and source columns.\n',
  },
  {
    path: 'results/mail.md',
    content:
      '# Report delivery draft\n\nMock deliverable. Waiting for operator confirmation before preparing report delivery. No email has been sent.\n',
  },
  {
    path: 'src/server/auth.ts',
    changed: true,
    content: `import { getSession } from "./session";\n\nexport async function authorize(req) {\n  const token = req.headers.authorization;\n  const session = await getSession(token);\n\n  if (!session || session.expiresAt < Date.now()) {\n    throw new Error("Invalid session");\n  }\n\n  return {\n    userId: session.userId,\n    role: session.role,\n  };\n}\n`,
  },
  { path: 'src/server/index.ts', content: 'export { authorize } from "./auth";\n' },
  { path: 'src/server/routes.ts', content: 'export const routes = ["/rpc", "/ws", "/blobs"];\n' },
  {
    path: 'src/tests/auth.test.ts',
    changed: true,
    content:
      'import { describe, it, expect } from "vitest";\n\ndescribe("session", () => {\n  it("rejects expired sessions", () => {\n    expect(true).toBe(true);\n  });\n});\n',
  },
  { path: 'package.json', content: '{\n  "name": "excalibur",\n  "private": true\n}\n' },
  { path: 'tsconfig.json', content: '{\n  "compilerOptions": {\n    "strict": true\n  }\n}\n' },
]
export const getProjectFiles = () => files
export const getFile = (path: string) => files.find((file) => file.path === path)

export const mockChanges: Record<string, FileChange> = {
  'src/server/auth.ts': {
    path: 'src/server/auth.ts',
    status: 'modified',
    added: 3,
    removed: 1,
    content: getFile('src/server/auth.ts')!.content,
    hunks: [
      {
        old_start: 7,
        old_lines: 1,
        new_start: 7,
        new_lines: 3,
        lines: [
          '-  if (!session) return null;',
          '+  if (!session || session.expiresAt < Date.now()) {',
          '+    throw new Error("Invalid session");',
          '+  }',
        ],
      },
    ],
  },
}
