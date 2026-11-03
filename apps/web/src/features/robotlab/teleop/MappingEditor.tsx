import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { robotlabCall } from './api'
import { useRobotText } from './i18n'
// Reuse the application's locally configured Monaco loader and workers.
const Editor = lazy(async () => {
  await import('@/features/files/CodeEditor')
  return import('@monaco-editor/react')
})
export function MappingEditor({ session }: { session: string }) {
  return <SessionMappingEditor key={session} session={session} />
}
function SessionMappingEditor({ session }: { session: string }) {
  const text = useRobotText()
  const [open, setOpen] = useState(false)
  const [code, setCode] = useState('')
  const [source, setSource] = useState<'default' | 'custom'>()
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [reading, setReading] = useState(false)
  const generation = useRef(0)
  useEffect(
    () => () => {
      generation.current++
    },
    [],
  )
  async function load(epoch: number) {
    setReading(true)
    try {
      const mapping = await robotlabCall('robotlab.teleop.get', { session })
      if (generation.current === epoch) {
        setCode(mapping.code)
        setSource(mapping.source)
      }
    } finally {
      if (generation.current === epoch) setReading(false)
    }
  }
  async function save(value: string | null) {
    const epoch = generation.current
    setSaving(true)
    setError('')
    try {
      const result = await robotlabCall('robotlab.teleop.mapping', { session, code: value })
      if (!result.ok) throw new Error(result.errors?.join('\n') || 'Mapping rejected')
      await load(epoch)
    } catch (e) {
      if (generation.current === epoch) setError(String(e))
    } finally {
      if (generation.current === epoch) setSaving(false)
    }
  }
  return (
    <div className="robot-capture-mapping">
      {source && <p>{text(source === 'default' ? 'defaultMapping' : 'customMapping')}</p>}
      <button
        disabled={saving}
        onClick={() => {
          const epoch = ++generation.current
          setOpen(!open)
          setError('')
          if (!open)
            void load(epoch).catch((cause: unknown) => {
              if (generation.current === epoch) setError(String(cause))
            })
        }}
      >
        {text(open ? 'close' : 'mapping')}
      </button>
      {open && reading ? (
        <PanelSkeleton shape="editor" label={text('mapping')} />
      ) : (
        open && (
          <>
            <div style={{ height: 260 }}>
              <Suspense fallback={<PanelSkeleton shape="editor" label={text('mapping')} />}>
                <Editor
                  language="python"
                  value={code}
                  onChange={(v) => setCode(v ?? '')}
                  loading={<PanelSkeleton shape="editor" label={text('mapping')} />}
                  options={{
                    automaticLayout: true,
                    minimap: { enabled: false },
                    ariaLabel: 'map_pose',
                  }}
                />
              </Suspense>
            </div>
            <button disabled={saving || !source || !code.trim()} onClick={() => void save(code)}>
              {text('save')}
            </button>
            <button disabled={saving || !source} onClick={() => void save(null)}>
              {text('reset')}
            </button>
          </>
        )
      )}
      {error && (
        <p role="alert">
          {!source && text('unknownMapping')} {error}
        </p>
      )}
    </div>
  )
}
