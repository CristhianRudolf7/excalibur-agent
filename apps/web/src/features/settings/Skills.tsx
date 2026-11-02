import { loadingLazy as lazy, LoadingFallback } from '@/features/shell/loading'
import { useLoading } from '@/components/shared'
import { FieldsSkeleton, SkillsSkeleton } from './Skeletons'
import { Suspense, useEffect, useRef, useState } from 'react'
import type React from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Sparkles, ArrowUpRight, Ellipsis, Pencil, Trash2, Search } from 'lucide-react'
import { rpc, RpcError } from '@/lib/rpc/client'
import type { SkillDetail as SkillDetailData, SkillSummary } from '@/lib/rpc/contract'
import { Markdown } from '@/components/markdown/Markdown'
import { Dialog } from '@/components/ui/dialog'
import { filterSkills, groupSkills, type SkillFilters } from './catalog'
import { Feedback } from './feedback'
import { FilterSelect } from './FilterSelect'
import { Categories } from './Categories'
const MarkdownEditor = lazy(() =>
  import('@/features/files/CodeEditor').then((m) => ({ default: m.MarkdownEditor })),
)
const sourceLabels = {
  builtin: 'settingsBuiltin',
  user: 'settingsUser',
  installed: 'settingsInstalled',
  project: 'settingsProject',
}
const listKey = ['settings-skills'] as const
function diagnostics(error: Error) {
  if (!(error instanceof RpcError) || error.code !== -32602) return null
  const data = error.data
  if (Array.isArray(data))
    return data.filter((item) => typeof item === 'string').join('\n') || error.message
  if (data && typeof data === 'object' && 'diagnostics' in data && Array.isArray(data.diagnostics))
    return data.diagnostics.filter((item) => typeof item === 'string').join('\n') || error.message
  return error.message
}
function SkillDetail({
  name,
  onClose,
  onChanged,
  toggle,
  busy,
  toggleError,
  action,
}: {
  name: string
  onClose: () => void
  onChanged: () => void
  toggle: (skill: SkillSummary) => void
  busy: boolean
  toggleError: boolean
  action: 'view' | 'edit' | 'delete'
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const detail = useQuery({
    queryKey: ['skill', name],
    queryFn: ({ signal }) => rpc.call('skills.get', { name }, { signal }),
    retry: false,
  })
  const [editing, setEditing] = useState(action === 'edit')
  const [content, setContent] = useState<string | null>(null)
  const [confirm, setConfirm] = useState(action === 'delete')
  const save = useMutation({
    mutationFn: () =>
      rpc.call('skills.update', { name, content: content ?? detail.data?.content ?? '' }),
    onMutate: async () => {
      await client.cancelQueries({ queryKey: ['skill', name] })
      const previous = client.getQueryData<SkillDetailData>(['skill', name])
      if (previous)
        client.setQueryData(['skill', name], { ...previous, content: content ?? previous.content })
      return previous
    },
    onError: (_error, _variables, previous) => {
      if (previous) client.setQueryData(['skill', name], previous)
    },
    onSuccess: async () => {
      setEditing(false)
      onChanged()
      await client.invalidateQueries({ queryKey: ['skill', name] })
    },
  })
  const remove = useMutation({
    mutationFn: () => rpc.call('skills.delete', { name }),
    onMutate: async () => {
      await client.cancelQueries({ queryKey: listKey })
      const previous = client.getQueryData<{ skills: SkillSummary[] }>(listKey)
      client.setQueryData(listKey, {
        skills: previous?.skills.filter((skill) => skill.name !== name) ?? [],
      })
      return previous
    },
    onError: (_error, _vars, previous) => {
      if (previous) client.setQueryData(listKey, previous)
    },
    onSuccess: () => {
      onChanged()
      onClose()
    },
  })
  const detailLoading = useLoading(detail.isPending)
  const skill = detailLoading.busy ? undefined : detail.data
  return (
    <Dialog title="settingsDetail" onClose={onClose}>
      <div className="skill-detail">
        <Feedback loading={detailLoading} query={detail} />
        {toggleError && <p role="alert">{t('settingsOffline')}</p>}
        {skill && (
          <>
            <h3>{skill.name}</h3>
            <p>{skill.description}</p>
            <div className="skill-meta">
              <span>{t(sourceLabels[skill.source])}</span>
              <span>{skill.version}</span>
              <span>
                {skill.state === 'enabled'
                  ? t('settingsEnabled')
                  : skill.state === 'disabled'
                    ? t('settingsDisabled')
                    : skill.state}
              </span>
            </div>
            <h4>{t('settingsTools')}</h4>
            <p>{skill.tools.join(', ') || t('settingsEmpty')}</p>
            {skill.diagnostics.length > 0 && (
              <pre role="status">{skill.diagnostics.join('\n')}</pre>
            )}
            {editing ? (
              <>
                <label>{t('settingsEditor')}</label>
                {skill.source === 'builtin' && (
                  <p className="skill-override">{t('settingsOverride')}</p>
                )}
                <div className="skill-editor">
                  <Suspense
                    fallback={
                      <LoadingFallback>
                        <FieldsSkeleton editor />
                      </LoadingFallback>
                    }
                  >
                    <MarkdownEditor value={content ?? skill.content} onChange={setContent} />
                  </Suspense>
                </div>
                <div className="settings-actions">
                  <button
                    aria-busy={save.isPending}
                    disabled={save.isPending}
                    onClick={() => save.mutate()}
                  >
                    {save.isPending && <span className="pending-spinner" aria-hidden="true" />}
                    {t('settingsSave')}
                  </button>
                  <button
                    disabled={save.isPending}
                    onClick={() => {
                      setEditing(false)
                      save.reset()
                    }}
                  >
                    {t('settingsCancel')}
                  </button>
                </div>
              </>
            ) : (
              <>
                <Markdown>{skill.content}</Markdown>
                <div className="settings-actions">
                  {(skill.editable || skill.source === 'builtin') && (
                    <button
                      onClick={() => {
                        setContent(skill.content)
                        save.reset()
                        setEditing(true)
                      }}
                    >
                      {t('settingsEdit')}
                    </button>
                  )}
                  {skill.source === 'builtin' ? (
                    <button
                      disabled={busy || skill.state === 'disabled'}
                      onClick={() => toggle(skill)}
                    >
                      {t('settingsDisable', { name: skill.name })}
                    </button>
                  ) : (
                    skill.deletable && (
                      <button onClick={() => setConfirm(true)}>{t('settingsDelete')}</button>
                    )
                  )}
                </div>
              </>
            )}
            {save.isSuccess && <p role="status">{t('settingsSaved')}</p>}
            {save.error && (
              <pre role="alert">{diagnostics(save.error) ?? t('settingsOffline')}</pre>
            )}
            {confirm && (
              <section className="skill-delete" aria-label={t('settingsDeleteTitle')}>
                <h4>{t('settingsDeleteTitle')}</h4>
                <p>{t('settingsDeleteNote', { name })}</p>
                <div className="settings-actions">
                  <button disabled={remove.isPending} onClick={() => remove.mutate()}>
                    {t('settingsDelete')}
                  </button>
                  <button disabled={remove.isPending} onClick={() => setConfirm(false)}>
                    {t('settingsCancel')}
                  </button>
                </div>
                {remove.isError && <p role="alert">{t('settingsOffline')}</p>}
              </section>
            )}
          </>
        )}
      </div>
    </Dialog>
  )
}
export function Skills() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [filters, setFilters] = useState<SkillFilters>({ text: '', source: '', state: '' })
  const [selected, setSelected] = useState<string | null>(null)
  const [action, setAction] = useState<'view' | 'edit' | 'delete'>('view')
  const [category, setCategory] = useState('')
  const [columns, setColumns] = useState(() => (matchMedia('(min-width: 1100px)').matches ? 2 : 1))
  useEffect(() => {
    const media = matchMedia('(min-width: 1100px)')
    const update = () => setColumns(media.matches ? 2 : 1)
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])
  const open = (name: string, next: typeof action = 'view') => {
    setAction(next)
    setSelected(name)
  }
  const scroll = useRef<HTMLDivElement>(null)
  const groups = useQuery({
    queryKey: ['skill-groups'],
    queryFn: ({ signal }) => rpc.call('skills.groups', undefined, { signal }),
    retry: false,
  })
  const list = useQuery({
    queryKey: listKey,
    queryFn: ({ signal }) => rpc.call('skills.list', {}, { signal }),
    retry: false,
  })
  const loading = useLoading(list.isPending || groups.isPending)
  const refresh = () => {
    void client.invalidateQueries({ queryKey: listKey })
    void client.invalidateQueries({ queryKey: ['skill-groups'] })
  }
  const state = useMutation({
    mutationFn: (skill: SkillSummary) =>
      rpc.call('skills.set_state', {
        name: skill.name,
        state: skill.state === 'enabled' ? 'disabled' : 'enabled',
      }),
    onMutate: async (skill) => {
      await client.cancelQueries({ queryKey: listKey })
      const previous = client.getQueryData<{ skills: SkillSummary[] }>(listKey)
      client.setQueryData(listKey, {
        skills:
          previous?.skills.map((item) =>
            item.name === skill.name
              ? { ...item, state: skill.state === 'enabled' ? 'disabled' : 'enabled' }
              : item,
          ) ?? [],
      })
      return previous
    },
    onError: (_error, _skill, previous) => {
      if (previous) client.setQueryData(listKey, previous)
    },
    onSuccess: (skill) => {
      client.setQueryData<{ skills: SkillSummary[] }>(listKey, (previous) => ({
        skills: previous?.skills.map((item) => (item.name === skill.name ? skill : item)) ?? [],
      }))
      void client.invalidateQueries({ queryKey: ['skill', skill.name] })
    },
  })
  const grouped = groupSkills(
    filterSkills(list.data?.skills ?? [], filters),
    groups.data?.groups ?? [],
  )
  const rows = grouped
    .filter((group) => !category || group.id === category)
    .flatMap((group) => [
      { type: 'group' as const, ...group },
      ...Array.from({ length: Math.ceil(group.skills.length / columns) }, (_, index) => ({
        type: 'skills' as const,
        skills: group.skills.slice(index * columns, (index + 1) * columns),
      })),
    ])
  const virtual = useVirtualizer({
    count: rows.length,
    getItemKey: (index) => {
      const row = rows[index]!
      return row.type === 'group'
        ? `group-${row.id}`
        : row.skills.map((skill) => skill.name).join(',')
    },
    getScrollElement: () => scroll.current,
    estimateSize: (index) => (rows[index]?.type === 'group' ? 56 : 178),
    overscan: 8,
  })
  return (
    <>
      {list.data && (
        <div className="skills-summary">
          <div>
            <span>{t('settingsActiveSkills')}</span>
            <strong>
              {list.data.skills.filter((skill) => skill.state === 'enabled').length}
              <small> / {list.data.skills.length}</small>
            </strong>
          </div>
          <Sparkles aria-hidden="true" />
        </div>
      )}
      <p className="settings-description">{t('settingsCatalog')}</p>
      <div className="settings-toolbar skill-filters">
        <label className="settings-search">
          <Search size={17} aria-hidden="true" />
          <input
            aria-label={t('settingsSearch')}
            placeholder={t('settingsSearch')}
            value={filters.text}
            onChange={(event) => {
              setFilters({ ...filters, text: event.target.value })
              scroll.current?.scrollTo(0, 0)
            }}
          />
        </label>
        <FilterSelect
          label={t('settingsSource')}
          value={filters.source}
          onChange={(source) => {
            setFilters({ ...filters, source })
            scroll.current?.scrollTo(0, 0)
          }}
          options={[
            { value: '', label: t('settingsAll') },
            ...Object.entries(sourceLabels).map(([value, label]) => ({ value, label: t(label) })),
          ]}
        />
        <FilterSelect
          label={t('settingsStatus')}
          value={filters.state}
          onChange={(state) => {
            setFilters({ ...filters, state })
            scroll.current?.scrollTo(0, 0)
          }}
          options={[
            { value: '', label: t('settingsAll') },
            { value: 'enabled', label: t('settingsEnabled') },
            { value: 'disabled', label: t('settingsDisabled') },
            { value: 'other', label: t('settingsOther') },
          ]}
        />
      </div>
      <Categories
        groups={groups.data?.groups ?? []}
        total={list.data?.skills.length ?? 0}
        value={category}
        onChange={(value) => {
          setCategory(value)
          scroll.current?.scrollTo(0, 0)
        }}
      />
      <Feedback query={{ ...groups, isPending: false }} />
      <Feedback
        loading={loading}
        query={{ ...list, isPending: list.isPending || groups.isPending }}
        skeleton={<SkillsSkeleton />}
      />
      {state.isError && <p role="alert">{t('settingsOffline')}</p>}
      {list.isSuccess && groups.isSuccess && !rows.length && (
        <div className="settings-card">{t('settingsEmpty')}</div>
      )}
      <div className="skill-list" ref={scroll}>
        <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
          {!loading.busy &&
            virtual.getVirtualItems().map((item) => {
              const row = rows[item.index]!
              return (
                <div
                  key={
                    row.type === 'group'
                      ? `group-${row.id}`
                      : row.skills.map((skill) => skill.name).join(',')
                  }
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    height: item.size,
                    transform: `translateY(${item.start}px)`,
                  }}
                >
                  {row.type === 'group' ? (
                    <h3 className="skill-group">
                      {row.group
                        ? i18n.language.startsWith('es')
                          ? row.group.name_es
                          : row.group.name_en
                        : row.id || t('settingsUncategorized')}
                      <span>
                        {row.skills.length}
                        {row.group ? ` / ${row.group.count}` : ''}
                      </span>
                    </h3>
                  ) : (
                    <div
                      className="skill-card-row"
                      style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}
                    >
                      {row.skills.map((skill) => (
                        <article
                          key={skill.name}
                          className="skill-card"
                          style={{ animationDelay: `${Math.min(item.index, 8) * 35}ms` }}
                        >
                          <button className="skill-open" onClick={() => open(skill.name)}>
                            <span
                              className="skill-mark"
                              aria-hidden="true"
                              style={{ '--mark-hue': markHue(skill.name) } as React.CSSProperties}
                            >
                              {skill.name
                                .replace(/[^a-z0-9]/gi, '')
                                .charAt(0)
                                .toUpperCase() || '?'}
                            </span>
                            <div>
                              <strong>
                                {skill.name}
                                <ArrowUpRight />
                              </strong>
                              <p>{skill.description}</p>
                              <span>
                                {t(sourceLabels[skill.source])} · {skill.version}
                              </span>
                            </div>
                          </button>
                          <button
                            role="switch"
                            aria-checked={skill.state === 'enabled'}
                            aria-label={t(
                              skill.state === 'enabled' ? 'settingsDisable' : 'settingsEnable',
                              { name: skill.name },
                            )}
                            disabled={state.isPending}
                            className="skill-switch"
                            onClick={() => state.mutate(skill)}
                          >
                            <span />
                          </button>
                          <details className="skill-menu">
                            <summary aria-label={t('settingsActions', { name: skill.name })}>
                              <Ellipsis size={16} />
                            </summary>
                            <div>
                              {(skill.editable || skill.source === 'builtin') && (
                                <button onClick={() => open(skill.name, 'edit')}>
                                  <Pencil size={14} />
                                  {t('settingsEdit')}
                                </button>
                              )}
                              {skill.deletable && (
                                <button onClick={() => open(skill.name, 'delete')}>
                                  <Trash2 size={14} />
                                  {t('settingsDelete')}
                                </button>
                              )}
                            </div>
                          </details>
                          <span className="skill-state">
                            {skill.state === 'enabled'
                              ? t('settingsEnabled')
                              : skill.state === 'disabled'
                                ? t('settingsDisabled')
                                : skill.state}
                          </span>
                        </article>
                      ))}
                    </div>
                  )}
                </div>
              )
            })}
        </div>
      </div>
      {selected && (
        <SkillDetail
          key={selected}
          name={selected}
          action={action}
          onClose={() => setSelected(null)}
          onChanged={refresh}
          toggle={(skill) => state.mutate(skill)}
          busy={state.isPending}
          toggleError={state.isError}
        />
      )}
    </>
  )
}

const markHues = [222, 199, 252, 168, 280, 210]

function markHue(name: string) {
  let hash = 0
  for (const char of name) hash = (hash * 31 + char.charCodeAt(0)) | 0
  return markHues[Math.abs(hash) % markHues.length]
}
