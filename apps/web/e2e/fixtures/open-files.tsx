/** Browser harness for the FE-B seam; the production shell is owned by FE-A. */
import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  openWorkspaceFile,
  registerPanelOpener,
  type PanelView,
} from '../../src/features/files/open'
import { Files } from '../../src/features/files/Files'
import { Spreadsheet } from '../../src/features/spreadsheet/Spreadsheet'
import { DocumentViewer } from '../../src/features/viewer/DocumentViewer'
import '../../src/index.css'
import i18n from '../../src/lib/i18n'
void i18n.changeLanguage('en')

export function Harness() {
  const [view, setView] = useState<PanelView>('document')
  registerPanelOpener((next) => {
    setView(next)
    history.replaceState(null, '', `?panel=${next}`)
  })
  return (
    <QueryClientProvider client={client}>
      <div
        style={{
          height: '100dvh',
          display: 'flex',
          flexDirection: 'column',
          background: 'var(--background)',
          color: 'var(--foreground)',
        }}
      >
        <nav
          aria-label="Workspace fixtures"
          style={{ display: 'flex', flexWrap: 'wrap', gap: 8, padding: 12 }}
        >
          {['real.ts', 'budget.xlsx', 'report.pdf', 'archive.bin', 'photo.png', 'missing.pdf'].map(
            (path) => (
              <button key={path} onClick={() => void openWorkspaceFile(path)}>
                {path}
              </button>
            ),
          )}
        </nav>
        <main style={{ flex: 1, minHeight: 0, overflow: 'hidden' }}>
          {view === 'files' ? <Files /> : view === 'sheet' ? <Spreadsheet /> : <DocumentViewer />}
        </main>
      </div>
    </QueryClientProvider>
  )
}
const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
createRoot(document.getElementById('root')!).render(<Harness />)
