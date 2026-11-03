import { CollectionIcon } from './CollectionIcon'
import { useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Plus, Search, ArrowUp, ArrowDown } from 'lucide-react'
import type { Collection, Filter, Field, RpcMethods } from '@/lib/rpc/contract'
import { rpc } from '@/lib/rpc/client'
import { Button } from '@/components/ui/button'
import { useProfile } from '@/features/profile/api'
import { dataKey } from './api'
import { DataError, DataLoading, ImportButton, CollectionSummary } from './MyData'
import { operators, formatValue, fieldValue } from './values'

import { changeList, initialListState, type ListState } from './list-state'

function FilterValue({
  field,
  filter,
  onChange,
}: {
  field: Field
  filter: Filter
  onChange: (value: unknown) => void
}) {
  const { t } = useTranslation()
  if (filter.op === 'is_empty' || filter.op === 'not_empty') return null
  if (field.type === 'boolean')
    return (
      <select
        aria-label={t('dataValue')}
        value={String(filter.value ?? true)}
        onChange={(event) => onChange(event.target.value === 'true')}
      >
        <option value="true">{t('dataYes')}</option>
        <option value="false">{t('dataNo')}</option>
      </select>
    )
  if (field.type === 'select')
    return (
      <select
        aria-label={t('dataValue')}
        value={String(filter.value ?? '')}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">{t('dataChoose')}</option>
        {field.options?.map((option) => (
          <option key={option}>{option}</option>
        ))}
      </select>
    )
  return (
    <input
      aria-label={t('dataValue')}
      type={field.type === 'date' ? 'date' : field.type === 'datetime' ? 'datetime-local' : 'text'}
      inputMode={['money', 'number', 'integer'].includes(field.type) ? 'decimal' : undefined}
      value={Array.isArray(filter.value) ? filter.value.join(', ') : String(filter.value ?? '')}
      onChange={(event) => {
        const raw = event.target.value
        if (filter.op === 'in') onChange(raw.split(',').map((item) => item.trim()))
        else onChange(raw)
      }}
    />
  )
}
export function RecordList({ collection }: { collection: Collection }) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const profile = useProfile()
  const [state, setState] = useState(initialListState)
  const [visible, setVisible] = useState(() => collection.fields.map((field) => field.id))
  const [notice, setNotice] = useState('')
  const [exporting, setExporting] = useState(false)
  const activeFilters = state.filters.flatMap((filter) => {
    if (filter.op === 'is_empty' || filter.op === 'not_empty') return [filter]
    if (filter.value === '' || filter.value === undefined) return []
    const field = collection.fields.find((item) => item.id === filter.field)
    if (!field || filter.op === 'in') return [filter]
    try {
      const value = fieldValue(field, filter.value, i18n.language)
      return typeof value === 'number' && !Number.isFinite(value) ? [] : [{ ...filter, value }]
    } catch {
      return []
    }
  })
  const params: RpcMethods['records.list']['params'] = {
    collection: collection.id,
    query: state.query || undefined,
    filters: activeFilters,
    sort: state.sort,
    limit: 25,
    cursor: state.cursors.at(-1),
  }
  const query = useQuery({
    queryKey: [...dataKey, collection.id, 'rows', params],
    placeholderData: keepPreviousData,
    queryFn: ({ signal }) => rpc.call('records.list', params, { signal }),
  })
  const fields = collection.fields.filter((field) => visible.includes(field.id))
  const change = (patch: Partial<Omit<ListState, 'cursors'>>) =>
    setState((previous) => changeList(previous, patch))
  const open = (id: string) =>
    void navigate({ to: '/data/$collection/$id', params: { collection: collection.id, id } })
  const exportFile = async (format: 'csv' | 'xlsx') => {
    setExporting(true)
    setNotice('')
    try {
      const file = await rpc.call('collections.export', {
        id: collection.id,
        format,
        filters: activeFilters,
      })
      const anchor = document.createElement('a')
      anchor.href = `/blobs/${encodeURIComponent(file.blob_digest)}`
      anchor.download = file.name
      document.body.append(anchor)
      anchor.click()
      anchor.remove()
      setNotice(t('dataExported'))
    } catch {
      setNotice(t('dataActionError'))
    } finally {
      setExporting(false)
    }
  }
  return (
    <>
      <header className="data-header">
        <div>
          <h1 className="data-collection-title">
            <CollectionIcon name={collection.icon} size={28} />
            {collection.name}
          </h1>
          <p>{collection.description}</p>
        </div>
        <div className="data-actions">
          <Button variant="primary" asChild>
            <Link to="/data/$collection/new" params={{ collection: collection.id }}>
              <Plus size={16} />
              {t('dataCreate')}
            </Link>
          </Button>
          <ImportButton collection={collection} />
          {(['xlsx', 'csv'] as const).map((format) => (
            <Button key={format} disabled={exporting} onClick={() => void exportFile(format)}>
              {t('dataExport', { format: format.toUpperCase() })}
            </Button>
          ))}
        </div>
      </header>
      <CollectionSummary collection={collection} />
      <div className="data-toolbar">
        <label className="data-search">
          <Search size={17} aria-hidden="true" />
          <input
            type="search"
            aria-label={t('dataSearch')}
            placeholder={t('dataSearch')}
            value={state.query}
            onChange={(event) => change({ query: event.target.value })}
          />
        </label>
        <select
          className="data-mobile-sort"
          aria-label={t('dataSortFields')}
          value={state.sort[0] ? `${state.sort[0].field}:${state.sort[0].dir}` : ''}
          onChange={(event) => {
            const [field, dir] = event.target.value.split(':')
            change({ sort: field ? [{ field, dir: dir === 'desc' ? 'desc' : 'asc' }] : [] })
          }}
        >
          <option value="">{t('dataSortFields')}</option>
          {collection.fields.flatMap((field) =>
            (['asc', 'desc'] as const).map((dir) => (
              <option key={`${field.id}:${dir}`} value={`${field.id}:${dir}`}>
                {field.name} · {t(dir === 'asc' ? 'dataAscending' : 'dataDescending')}
              </option>
            )),
          )}
        </select>
        <details className="data-columns">
          <summary>{t('dataColumns')}</summary>
          <div>
            {collection.fields.map((field) => (
              <label key={field.id}>
                <input
                  type="checkbox"
                  checked={visible.includes(field.id)}
                  disabled={visible.length === 1 && visible.includes(field.id)}
                  onChange={(event) =>
                    setVisible(
                      event.target.checked
                        ? [...visible, field.id]
                        : visible.filter((id) => id !== field.id),
                    )
                  }
                />
                {field.name}
              </label>
            ))}
          </div>
        </details>
        <Button
          disabled={!collection.fields.length}
          onClick={() => {
            const field = collection.fields[0]
            if (field)
              change({
                filters: [
                  ...state.filters,
                  { field: field.id, op: 'eq', value: field.type === 'boolean' ? true : '' },
                ],
              })
          }}
        >
          {t('dataFilter')}
        </Button>
      </div>
      <div className="data-filters">
        {state.filters.map((filter, index) => {
          const field = collection.fields.find((item) => item.id === filter.field)
          if (!field) return null
          const update = (patch: Partial<Filter>) =>
            change({
              filters: state.filters.map((item, position) =>
                position === index ? { ...item, ...patch } : item,
              ),
            })
          return (
            <div key={index} className="data-filter">
              <select
                aria-label={t('dataField')}
                value={filter.field}
                onChange={(event) => {
                  const next = collection.fields.find((item) => item.id === event.target.value)
                  update({
                    field: event.target.value,
                    op: 'eq',
                    value: next?.type === 'boolean' ? true : '',
                  })
                }}
              >
                {collection.fields.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
              <select
                aria-label={t('dataOperator')}
                value={filter.op}
                onChange={(event) => update({ op: event.target.value as Filter['op'] })}
              >
                {operators(field).map((op) => (
                  <option key={op} value={op}>
                    {t(`data${op[0]!.toUpperCase()}${op.slice(1)}`)}
                  </option>
                ))}
              </select>
              <FilterValue field={field} filter={filter} onChange={(value) => update({ value })} />
              <Button
                aria-label={t('dataRemoveFilter')}
                onClick={() =>
                  change({ filters: state.filters.filter((_, position) => position !== index) })
                }
              >
                ×
              </Button>
            </div>
          )
        })}
      </div>
      <p role="status" className="data-notice">
        {notice}
      </p>
      {query.isPending ? (
        <DataLoading />
      ) : query.isError ? (
        <DataError retry={() => void query.refetch()} />
      ) : (
        <>
          <p className="data-total">{t('dataRecords', { count: query.data.total })}</p>
          {!query.data.records.length ? (
            <div className="data-empty">
              <h2>{t(state.query || state.filters.length ? 'dataNoResults' : 'dataNoRecords')}</h2>
            </div>
          ) : (
            <>
              <div className="data-table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      {fields.map((field) => {
                        const sorted = state.sort.find((item) => item.field === field.id)
                        return (
                          <th
                            key={field.id}
                            aria-sort={
                              sorted ? (sorted.dir === 'asc' ? 'ascending' : 'descending') : 'none'
                            }
                          >
                            <button
                              aria-label={t('dataSort', { name: field.name })}
                              onClick={() =>
                                change({
                                  sort: [
                                    {
                                      field: field.id,
                                      dir: sorted?.dir === 'asc' ? 'desc' : 'asc',
                                    },
                                  ],
                                })
                              }
                            >
                              {field.name}
                              {sorted &&
                                (sorted.dir === 'asc' ? (
                                  <ArrowUp size={14} />
                                ) : (
                                  <ArrowDown size={14} />
                                ))}
                            </button>
                          </th>
                        )
                      })}
                      <th>
                        <span className="sr-only">{t('dataView')}</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {query.data.records.map((record) => (
                      <tr key={record.id} onClick={() => open(record.id)}>
                        {fields.map((field) => (
                          <td key={field.id}>
                            {field.type === 'boolean' ? (
                              <span className="data-badge">
                                {t(record.values[field.id] ? 'dataYes' : 'dataNo')}
                              </span>
                            ) : (
                              formatValue(
                                field,
                                record.values[field.id],
                                i18n.language,
                                profile.data?.timezone,
                              )
                            )}
                          </td>
                        ))}
                        <td>
                          <Link
                            aria-label={`${t('dataView')}: ${String(record.values[collection.display_field] ?? record.id)}`}
                            to="/data/$collection/$id"
                            params={{ collection: collection.id, id: record.id }}
                            onClick={(event) => event.stopPropagation()}
                          >
                            <ArrowUp size={15} />
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="data-mobile-list">
                {query.data.records.map((record) => (
                  <Link
                    key={record.id}
                    to="/data/$collection/$id"
                    params={{ collection: collection.id, id: record.id }}
                    className="data-mobile-card"
                  >
                    <h2>{String(record.values[collection.display_field] ?? record.id)}</h2>
                    <dl>
                      {fields.map((field) => (
                        <div key={field.id}>
                          <dt>{field.name}</dt>
                          <dd>
                            {field.type === 'boolean'
                              ? t(record.values[field.id] ? 'dataYes' : 'dataNo')
                              : formatValue(
                                  field,
                                  record.values[field.id],
                                  i18n.language,
                                  profile.data?.timezone,
                                )}
                          </dd>
                        </div>
                      ))}
                    </dl>
                  </Link>
                ))}
              </div>
            </>
          )}
          <div className="data-pagination">
            <Button
              disabled={state.cursors.length <= 1}
              onClick={() =>
                setState((previous) => ({ ...previous, cursors: previous.cursors.slice(0, -1) }))
              }
            >
              {t('dataPrevious')}
            </Button>
            <Button
              disabled={query.isPlaceholderData || !query.data.next_cursor}
              onClick={() =>
                setState((previous) => ({
                  ...previous,
                  cursors: [...previous.cursors, query.data.next_cursor],
                }))
              }
            >
              {t('dataNext')}
            </Button>
          </div>
        </>
      )}
    </>
  )
}
