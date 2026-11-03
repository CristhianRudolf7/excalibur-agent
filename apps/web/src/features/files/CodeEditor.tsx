import { changeDecorations, hunkPositions, removedContent, removedZones } from './diff'
import '@/styles/redesign/changes.css'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useLoadingState } from '@/features/work/loading'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import Editor, { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor/editor/editor.api'
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'
import 'monaco-editor/languages/definitions/typescript/register'
import 'monaco-editor/languages/definitions/markdown/register'
import 'monaco-editor/languages/definitions/python/register'
monaco.languages.register({ id: 'json' })
import { useTranslation } from 'react-i18next'
import { useFiles } from './store'
import { getFile } from './mock-data'
self.MonacoEnvironment = { getWorker: () => new EditorWorker() }
loader.config({ monaco })
function applyPanelTheme(m: typeof monaco) {
  const style = getComputedStyle(document.documentElement)
  const token = (name: string) =>
    style
      .getPropertyValue(name)
      .trim()
      .replace(
        /^#([a-f0-9])([a-f0-9])([a-f0-9])$/i,
        (_, r: string, g: string, b: string) => `#${r}${r}${g}${g}${b}${b}`,
      )
  m.editor.defineTheme('linear', {
    base: document.documentElement.classList.contains('dark') ? 'vs-dark' : 'vs',
    inherit: true,
    rules: [],
    colors: {
      'editor.background': token('--surface'),
      'editor.foreground': token('--foreground'),
      'editor.selectionBackground': token('--accent-tint-strong'),
      'editorCursor.foreground': token('--primary-strong'),
      'editorLineNumber.foreground': token('--tertiary'),
      'editor.lineHighlightBackground': token('--muted'),
    },
  })
  m.editor.setTheme('linear')
}

function EditorFrame({ ready, children }: { ready: boolean; children: ReactNode }) {
  const { t } = useTranslation()
  const loading = useLoadingState(!ready)
  return (
    <div className="editor-loading-frame">
      {loading.visible && (
        <div className="editor-loading-overlay">
          <PanelSkeleton shape="editor" label={t('loadingEditor')} />
        </div>
      )}
      <div
        className="editor-loading-content"
        style={{ visibility: loading.holding ? 'hidden' : undefined }}
        inert={loading.holding}
      >
        {children}
      </div>
    </div>
  )
}

export default function CodeEditor({
  onCursor,
}: {
  onCursor: (line: number, column: number) => void
}) {
  const { t } = useTranslation()
  const [ready, setReady] = useState(false)
  const state = useFiles()
  const change = state.changes[state.selected]
  const file = change
    ? {
        path: change.path,
        content:
          change.status === 'deleted'
            ? removedContent(change)
            : (change.content ?? state.contents[state.selected]),
      }
    : (getFile(state.selected) ??
      (state.contents[state.selected] !== undefined
        ? { path: state.selected, content: state.contents[state.selected] }
        : undefined))
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null)
  const [hunk, setHunk] = useState(0)
  const positions = change ? hunkPositions(change) : []
  useEffect(() => {
    const editor = editorRef.current
    if (!editor || !ready) return
    const collection = editor.createDecorationsCollection(
      change ? changeDecorations(change, editor.getModel()?.getLineCount() ?? 1) : [],
    )
    // Removed lines render as read-only "ghost" rows between the new lines.
    const ids: string[] = []
    if (change)
      editor.changeViewZones((zones) => {
        for (const zone of removedZones(change)) {
          const node = document.createElement('div')
          node.className = 'diff-zone'
          for (const text of zone.lines) {
            const row = document.createElement('div')
            row.className = 'diff-zone-line'
            row.textContent = text || ' '
            node.append(row)
          }
          ids.push(
            zones.addZone({
              afterLineNumber: zone.afterLine,
              heightInLines: zone.lines.length,
              domNode: node,
            }),
          )
        }
      })
    return () => {
      collection.clear()
      editor.changeViewZones((zones) => ids.forEach((id) => zones.removeZone(id)))
    }
  }, [change, ready])
  const navigate = (direction: number) => {
    const next = (hunk + direction + positions.length) % positions.length
    setHunk(next)
    const editor = editorRef.current
    const line = Math.min(editor?.getModel()?.getLineCount() ?? 1, positions[next] ?? 1)
    editor?.setPosition({ lineNumber: line, column: 1 })
    editor?.revealLineInCenter(line)
  }
  if (!file) return <div className="empty-state">{t('noFiles')}</div>
  return (
    <>
      {change && (
        <div className="change-navigation">
          <span className="change-add">+{change.added}</span>
          <span className="change-del">−{change.removed}</span>
          <span className="change-by">· {t('changeBy')}</span>
          {change.truncated && <span>{t('changeTruncated')}</span>}
          <span role="status">
            {t('changePosition', {
              current: positions.length ? hunk + 1 : 0,
              total: positions.length,
            })}
          </span>
          <button
            disabled={!positions.length || file.content === undefined}
            aria-label={t('changePrevious')}
            onClick={() => navigate(-1)}
          >
            ↑
          </button>
          <button
            disabled={!positions.length || file.content === undefined}
            aria-label={t('changeNext')}
            onClick={() => navigate(1)}
          >
            ↓
          </button>
        </div>
      )}
      {file.content === undefined ? (
        <div className="change-preview">
          <p>{t('changeLarge')}</p>
          {change?.hunks.map((item, index) => (
            <pre
              key={index}
            >{`@@ -${item.old_start},${item.old_lines} +${item.new_start},${item.new_lines} @@\n${item.lines.join('\n')}`}</pre>
          ))}
        </div>
      ) : (
        <EditorFrame ready={ready}>
          <Editor
            path={file.path}
            language={file.path.endsWith('.json') ? 'json' : 'typescript'}
            value={state.edits[file.path] ?? file.content}
            loading={null}
            onChange={(value) => state.edit(file.path, value ?? '')}
            beforeMount={applyPanelTheme}
            theme="linear"
            onMount={(editor, m) => {
              setReady(true)
              const observer = new MutationObserver(() => applyPanelTheme(m))
              observer.observe(document.documentElement, {
                attributes: true,
                attributeFilter: ['class'],
              })
              editor.onDidDispose(() => observer.disconnect())
              onCursor(editor.getPosition()?.lineNumber ?? 1, editor.getPosition()?.column ?? 1)
              editor.onDidChangeCursorPosition((event) =>
                onCursor(event.position.lineNumber, event.position.column),
              )
              editorRef.current = editor
            }}
            options={{
              ariaLabel: t('codeEditor'),
              fontSize: 12,
              lineHeight: 18,
              fontFamily: 'ui-monospace, SFMono-Regular, monospace',
              minimap: { enabled: false },
              scrollBeyondLastLine: false,
              automaticLayout: true,
              wordWrap: 'on',
              padding: { top: 12 },
              lineNumbersMinChars: 3,
              glyphMargin: true,
              readOnly: change?.status === 'deleted',
              renderLineHighlight: 'none',
              tabSize: 2,
              overviewRulerLanes: 0,
              contextmenu: false,
            }}
          />
        </EditorFrame>
      )}
    </>
  )
}

/** Controlled Markdown editor shared by the settings catalog. */
export function MarkdownEditor({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const [ready, setReady] = useState(false)
  return (
    <EditorFrame ready={ready}>
      <Editor
        onMount={() => setReady(true)}
        language="markdown"
        value={value}
        onChange={(v) => onChange(v ?? '')}
        loading={null}
        theme={document.documentElement.classList.contains('dark') ? 'vs-dark' : 'vs'}
        options={{
          ariaLabel: t('settingsEditor'),
          automaticLayout: true,
          minimap: { enabled: false },
          wordWrap: 'on',
          scrollBeyondLastLine: false,
        }}
      />
    </EditorFrame>
  )
}

/** Controlled Python editor for tool-owned experiment scripts. */
export function PythonEditor({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const [ready, setReady] = useState(false)
  return (
    <EditorFrame ready={ready}>
      <Editor
        language="python"
        value={value}
        onChange={(next) => onChange(next ?? '')}
        loading={null}
        beforeMount={applyPanelTheme}
        theme="linear"
        onMount={() => setReady(true)}
        options={{
          ariaLabel: 'Python',
          automaticLayout: true,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          fontSize: 13,
          tabSize: 4,
        }}
      />
    </EditorFrame>
  )
}
