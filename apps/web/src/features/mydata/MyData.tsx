import { CollectionIcon } from './CollectionIcon'
import { DataNotice } from './notice'
import { notifyData } from './notice-store'
import { useRef, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Database, Plus, Upload, ArrowLeft, ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { rpc } from '@/lib/rpc/client'
import { useOpenAgentChat } from '@/features/agents/navigation'
import { useAgents } from '@/features/agents/store'
import { useProfile } from '@/features/profile/api'
import type { Collection, Field, RecordRow } from '@/lib/rpc/contract'
import { useCollections, useCollection, useRecord, dataKey, uploadDataFile } from './api'
import { RecordForm } from './RecordForm'
import { RecordList } from './RecordList'
import { DeleteRecord } from './DeleteRecord'
import { formatValue } from './values'
import { useDataAttachment } from './chat'
import './mydata.css'

export function DataLoading() {
  const { t } = useTranslation()
  return (
    <div className="data-skeleton" role="status" aria-label={t('dataLoading')}>
      <div />
      <div />
      <div />
    </div>
  )
}
export function DataError({ retry }: { retry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="data-empty" role="alert">
      <p>{t('dataError')}</p>
      <Button onClick={retry}>{t('dataRetry')}</Button>
    </div>
  )
}
export function ImportButton({ collection }: { collection?: Collection }) {
  const { t } = useTranslation()
  const input = useRef<HTMLInputElement>(null)
  const openChat = useOpenAgentChat()
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  return (
    <div>
      <Button disabled={busy} onClick={() => input.current?.click()}>
        <Upload size={16} aria-hidden="true" />
        {t(busy ? 'dataUploading' : 'dataImport')}
      </Button>
      <input
        ref={input}
        type="file"
        hidden
        accept=".csv,.xlsx,.xls,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-excel"
        aria-label={t('dataImport')}
        onChange={async (event) => {
          const file = event.target.files?.[0]
          if (!file) return
          setBusy(true)
          setFailed(false)
          try {
            const digest = await uploadDataFile(file)
            const agent = useAgents.getState().favorite_id
            useDataAttachment.getState().set({ agent, name: file.name, digest })
            openChat(
              t('dataImportPrompt', {
                name: file.name,
                digest,
                target: collection ? t('dataImportTarget', { name: collection.name }) : '',
              }),
              agent,
            )
          } catch {
            setFailed(true)
          } finally {
            setBusy(false)
            if (input.current) input.current.value = ''
          }
        }}
      />
      {failed && <p role="alert">{t('dataActionError')}</p>}
    </div>
  )
}
export function DataOverview() {
  const { t, i18n } = useTranslation()
  const query = useCollections()
  const openChat = useOpenAgentChat()
  return (
    <div className="data-page">
      <DataNotice />
      <header className="data-header">
        <div>
          <span className="data-eyebrow">
            <Database size={16} />
            {t('workspace')}
          </span>
          <h1>{t('myData')}</h1>
          <p>{t('dataSubtitle')}</p>
        </div>
        <div className="data-actions">
          <Button variant="primary" onClick={() => openChat(t('dataPrompt'))}>
            <Plus size={16} />
            {t('dataNew')}
          </Button>
          <ImportButton />
        </div>
      </header>
      {query.isPending ? (
        <DataLoading />
      ) : query.isError ? (
        <DataError retry={() => void query.refetch()} />
      ) : query.data.collections.length ? (
        <div className="data-grid">
          {query.data.collections.map((collection) => (
            <Link
              to="/data/$collection"
              params={{ collection: collection.id }}
              key={collection.id}
              className="data-collection-card"
            >
              <span className="data-collection-icon" aria-hidden="true">
                <CollectionIcon name={collection.icon} size={24} />
              </span>
              <span className="data-count">
                {t('dataRecords', { count: collection.record_count })}
              </span>
              <h2>{collection.name}</h2>
              <p>{collection.description}</p>
              <footer>
                <span>
                  {t('dataUpdated', {
                    date: new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(
                      new Date(collection.updated_at),
                    ),
                  })}
                </span>
                <ArrowUpRight size={16} />
              </footer>
            </Link>
          ))}
        </div>
      ) : (
        <div className="data-empty">
          <Database size={36} />
          <h2>{t('dataEmpty')}</h2>
          <p>{t('dataEmptyDetail')}</p>
          <Button onClick={() => openChat(t('dataPrompt'))}>{t('dataAsk')}</Button>
        </div>
      )}
    </div>
  )
}
function RelationValue({ field, value }: { field: Field; value: string }) {
  const schema = useCollection(field.target ?? '')
  const record = useRecord(field.target ?? '', value)
  const display = schema.data?.collection.display_field
  return field.target ? (
    <Link to="/data/$collection/$id" params={{ collection: field.target, id: value }}>
      {String((display && record.data?.record.values[display]) || value)}
    </Link>
  ) : (
    <span>{value}</span>
  )
}
export function RecordValue({ field, value }: { field: Field; value: unknown }) {
  const { t, i18n } = useTranslation()
  const profile = useProfile()
  if (value === null || value === undefined || value === '') return <span>—</span>
  if (field.type === 'boolean')
    return <span className="data-badge">{t(value ? 'dataYes' : 'dataNo')}</span>
  if (field.type === 'relation') return <RelationValue field={field} value={String(value)} />
  if (field.type === 'file' && typeof value === 'string')
    return (
      <div className="data-file-preview">
        <iframe sandbox="" src={`/blobs/${encodeURIComponent(value)}`} title={t('dataFile')} />
        <a href={`/blobs/${encodeURIComponent(value)}`} target="_blank" rel="noreferrer">
          {t('dataFile')}
        </a>
      </div>
    )
  if ((field.type === 'url' || field.type === 'email') && typeof value === 'string') {
    const href = field.type === 'email' ? `mailto:${value}` : value
    if (/^(https?:|mailto:)/i.test(href))
      return (
        <a href={href} target="_blank" rel="noreferrer">
          {value}
        </a>
      )
  }
  return <span>{formatValue(field, value, i18n.language, profile.data?.timezone)}</span>
}
function RelatedRecords({
  collection,
  field,
  records,
}: {
  collection: string
  field: string
  records: RecordRow[]
}) {
  const schema = useCollection(collection)
  return (
    <section>
      <h3>
        {schema.data?.collection.name ?? collection} ·{' '}
        {schema.data?.collection.fields.find((item) => item.id === field)?.name ?? field}
      </h3>
      <ul>
        {records.map((row) => (
          <li key={row.id}>
            <Link to="/data/$collection/$id" params={{ collection, id: row.id }}>
              {String(row.values[schema.data?.collection.display_field ?? ''] ?? row.id)}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}
function Detail({ collection, id }: { collection: Collection; id: string }) {
  const { t } = useTranslation()
  const query = useRecord(collection.id, id)
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState(false)
  if (query.isPending) return <DataLoading />
  if (query.isError) return <DataError retry={() => void query.refetch()} />
  const { record, related } = query.data
  return (
    <>
      <header className="data-header">
        <h1>{String(record.values[collection.display_field] ?? record.id)}</h1>
        <div className="data-actions">
          <Button asChild>
            <Link to="/data/$collection/$id/edit" params={{ collection: collection.id, id }}>
              {t('dataEdit')}
            </Link>
          </Button>
          <Button variant="destructive" onClick={() => setDeleting(true)}>
            {t('dataDelete')}
          </Button>
        </div>
      </header>
      <dl className="data-detail">
        {collection.fields.map((field) => (
          <div key={field.id}>
            <dt>{field.name}</dt>
            <dd>
              <RecordValue field={field} value={record.values[field.id]} />
            </dd>
          </div>
        ))}
      </dl>
      {related.some((group) => group.records.length) && (
        <section className="data-related">
          <h2>{t('dataRelated')}</h2>
          {related
            .filter((group) => group.records.length)
            .map((group) => (
              <RelatedRecords key={`${group.collection}:${group.field}`} {...group} />
            ))}
        </section>
      )}
      {deleting && (
        <DeleteRecord
          collection={collection}
          record={record}
          referenced={related.some((group) => group.records.length > 0)}
          onClose={() => setDeleting(false)}
          onDeleted={() =>
            void navigate({ to: '/data/$collection', params: { collection: collection.id } })
          }
        />
      )}
    </>
  )
}
function Editor({ collection, id }: { collection: Collection; id?: string }) {
  const query = useRecord(collection.id, id ?? '')
  const { t } = useTranslation()
  const navigate = useNavigate()
  const client = useQueryClient()
  if (id && query.isPending) return <DataLoading />
  if (id && query.isError) return <DataError retry={() => void query.refetch()} />
  return (
    <>
      <header className="data-header">
        <h1>{t(id ? 'dataEdit' : 'dataCreate')}</h1>
      </header>
      <RecordForm
        key={`${collection.id}:${id ?? 'new'}`}
        collection={collection}
        record={query.data?.record}
        onCancel={() =>
          void navigate({ to: '/data/$collection', params: { collection: collection.id } })
        }
        onSaved={(record) => {
          notifyData('dataSaved')
          client.setQueryData([...dataKey, collection.id, 'record', record.id], {
            record,
            related: query.data?.related ?? [],
          })
          void client.invalidateQueries({ queryKey: dataKey })
          void navigate({
            to: '/data/$collection/$id',
            params: { collection: collection.id, id: record.id },
          })
        }}
      />
    </>
  )
}
export function CollectionPage({
  collection: collectionId,
  id,
  mode = 'list',
}: {
  collection: string
  id?: string
  mode?: 'list' | 'view' | 'create' | 'edit'
}) {
  const { t } = useTranslation()
  const query = useCollection(collectionId)
  return (
    <div className="data-page">
      <DataNotice />
      <Link
        className="data-back"
        to={mode === 'list' ? '/data' : '/data/$collection'}
        params={{ collection: collectionId }}
      >
        <ArrowLeft size={16} />
        {t(mode === 'list' ? 'myData' : 'dataBack')}
      </Link>
      {query.isPending ? (
        <DataLoading />
      ) : query.isError ? (
        <DataError retry={() => void query.refetch()} />
      ) : mode === 'list' ? (
        <RecordList key={collectionId} collection={query.data.collection} />
      ) : mode === 'view' && id ? (
        <Detail key={`${collectionId}:${id}`} collection={query.data.collection} id={id} />
      ) : (
        <Editor collection={query.data.collection} id={mode === 'edit' ? id : undefined} />
      )}
    </div>
  )
}
export function CollectionSummary({ collection }: { collection: Collection }) {
  const { i18n } = useTranslation()
  const query = useQuery({
    queryKey: [...dataKey, collection.id, 'summary'],
    queryFn: ({ signal }) => rpc.call('collections.summary', { id: collection.id }, { signal }),
    enabled: !!collection.summary?.length,
  })
  if (!collection.summary?.length) return null
  if (query.isPending) return <DataLoading />
  if (query.isError) return <DataError retry={() => void query.refetch()} />
  return (
    <div className="data-summary">
      {query.data.cards.map((card) => {
        const config = collection.summary?.find((item) => item.id === card.id)
        const series = card.series ?? []
        const min = Math.min(...series.map((point) => point.value), 0)
        const max = Math.max(...series.map((point) => point.value), 1)
        const plotY = (value: number) => 55 - ((value - min) / (max - min)) * 50
        return (
          <article key={card.id}>
            <h2>{card.title}</h2>
            <strong>
              {new Intl.NumberFormat(i18n.language).format(card.value)} {card.unit}
            </strong>
            {config?.chart && config.chart !== 'none' && series.length > 0 && (
              <figure className="data-chart" aria-label={card.title}>
                <svg
                  viewBox="0 0 200 60"
                  role="img"
                  aria-label={series.map((point) => `${point.label}: ${point.value}`).join(', ')}
                >
                  {config.chart === 'line' ? (
                    <polyline
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      points={series
                        .map(
                          (point, index) =>
                            `${(index * 190) / Math.max(series.length - 1, 1) + 5},${plotY(point.value)}`,
                        )
                        .join(' ')}
                    />
                  ) : config.chart === 'pie' ? (
                    series.map((point, index) => {
                      const total = series.reduce((sum, item) => sum + Math.abs(item.value), 0) || 1
                      const prior = series
                        .slice(0, index)
                        .reduce((sum, item) => sum + Math.abs(item.value), 0)
                      return (
                        <circle
                          key={point.label}
                          cx="100"
                          cy="30"
                          r="20"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="12"
                          opacity={1 - index / (series.length + 1)}
                          pathLength="100"
                          strokeDasharray={`${(Math.abs(point.value) / total) * 100} 100`}
                          strokeDashoffset={(-prior / total) * 100}
                        />
                      )
                    })
                  ) : (
                    series.map((point, index) => (
                      <rect
                        key={point.label}
                        x={(index * 200) / series.length + 2}
                        y={Math.min(plotY(point.value), plotY(0))}
                        width={Math.max(1, 200 / series.length - 4)}
                        height={Math.abs(plotY(point.value) - plotY(0))}
                        fill="currentColor"
                      />
                    ))
                  )}
                </svg>
                <figcaption>
                  {series.map((point) => (
                    <span key={point.label}>
                      {point.label}: {new Intl.NumberFormat(i18n.language).format(point.value)}
                    </span>
                  ))}
                </figcaption>
              </figure>
            )}
          </article>
        )
      })}
    </div>
  )
}
