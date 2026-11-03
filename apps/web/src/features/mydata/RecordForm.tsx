import { useState } from 'react'
import { useBlocker } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { rpc } from '@/lib/rpc/client'
import type { Collection, Field, RecordRow } from '@/lib/rpc/contract'
import { uploadDataFile, useCollection } from './api'
import { fieldValue, localDateTime, validationErrors } from './values'

function RelationPicker({
  field,
  value,
  onChange,
  invalid,
}: {
  field: Field
  value: unknown
  onChange: (value: unknown) => void
  invalid?: boolean
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const target = field.target ?? ''
  const schema = useCollection(target)
  const records = useQuery({
    queryKey: ['mydata', target, 'picker', query],
    queryFn: ({ signal }) =>
      rpc.call('records.list', { collection: target, query, limit: 30 }, { signal }),
    enabled: !!target,
  })
  const selected = useQuery({
    queryKey: ['mydata', target, 'record', String(value)],
    queryFn: ({ signal }) =>
      rpc.call('records.get', { collection: target, id: String(value) }, { signal }),
    enabled: !!target && !!value,
  })
  const rows = [...(records.data?.records ?? [])]
  if (selected.data && !rows.some((row) => row.id === value)) rows.unshift(selected.data.record)
  const label = (row: RecordRow) =>
    String(row.values[schema.data?.collection.display_field ?? ''] ?? row.id)
  return (
    <div className="data-relation-picker">
      <input
        type="search"
        aria-label={t('dataRelationSearch', { name: field.name })}
        placeholder={t('dataSearch')}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <select
        id={`field-${field.id}`}
        aria-invalid={invalid}
        aria-describedby={invalid ? `error-${field.id}` : undefined}
        value={String(value ?? '')}
        onChange={(event) => onChange(event.target.value)}
        required={field.required}
      >
        <option value="">{t('dataChoose')}</option>
        {rows.map((row) => (
          <option key={row.id} value={row.id}>
            {label(row)}
          </option>
        ))}
      </select>
      {records.isError && <p role="alert">{t('dataError')}</p>}
    </div>
  )
}
export function FieldInput({
  field,
  value,
  onChange,
  invalid,
  onBusy,
}: {
  field: Field
  value: unknown
  onChange: (value: unknown) => void
  invalid?: boolean
  onBusy?: (busy: boolean) => void
}) {
  const { t, i18n } = useTranslation()
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState(false)
  const shared = {
    id: `field-${field.id}`,
    required: field.required,
    'aria-invalid': invalid,
    'aria-describedby': invalid ? `error-${field.id}` : undefined,
  }
  if (field.type === 'relation')
    return <RelationPicker field={field} value={value} onChange={onChange} invalid={invalid} />
  if (field.type === 'boolean')
    return (
      <input
        {...shared}
        required={false}
        type="checkbox"
        checked={value === true}
        onChange={(event) => onChange(event.target.checked)}
      />
    )
  if (field.type === 'long_text')
    return (
      <textarea
        {...shared}
        rows={4}
        value={String(value ?? '')}
        onChange={(event) => onChange(event.target.value)}
      />
    )
  if (field.type === 'select')
    return (
      <select
        {...shared}
        value={String(value ?? '')}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">{t('dataChoose')}</option>
        {field.options?.map((option) => (
          <option key={option}>{option}</option>
        ))}
      </select>
    )
  if (field.type === 'multi_select')
    return (
      <div
        id={shared.id}
        role="group"
        aria-label={field.name}
        aria-describedby={shared['aria-describedby']}
        className="data-chips"
      >
        {field.options?.map((option) => (
          <label key={option}>
            <input
              type="checkbox"
              checked={Array.isArray(value) && value.includes(option)}
              onChange={(event) =>
                onChange(
                  event.target.checked
                    ? [...(Array.isArray(value) ? value : []), option]
                    : (Array.isArray(value) ? value : []).filter((item) => item !== option),
                )
              }
            />
            {option}
          </label>
        ))}
      </div>
    )
  if (field.type === 'file')
    return (
      <div>
        <input
          {...shared}
          required={field.required && !value}
          type="file"
          disabled={uploading}
          onChange={async (event) => {
            const file = event.target.files?.[0]
            if (!file) return
            setUploading(true)
            onBusy?.(true)
            setUploadError(false)
            try {
              onChange(await uploadDataFile(file))
            } catch {
              setUploadError(true)
            } finally {
              setUploading(false)
              onBusy?.(false)
            }
          }}
        />
        {uploading && <p role="status">{t('dataUploading')}</p>}
        {uploadError && <p role="alert">{t('dataActionError')}</p>}
        {typeof value === 'string' && value && (
          <div>
            <a href={`/blobs/${encodeURIComponent(value)}`} target="_blank" rel="noreferrer">
              {t('dataFile')}
            </a>
            <Button onClick={() => onChange(null)}>{t('dataRemoveFile')}</Button>
          </div>
        )}
      </div>
    )
  const numeric = ['money', 'number', 'integer'].includes(field.type)
  const inputType =
    field.type === 'datetime'
      ? 'datetime-local'
      : ['email', 'url', 'date'].includes(field.type)
        ? field.type
        : 'text'
  const displayed =
    field.type === 'datetime'
      ? localDateTime(value)
      : numeric && typeof value === 'number'
        ? new Intl.NumberFormat(i18n.language, {
            useGrouping: false,
            maximumFractionDigits: 20,
          }).format(value)
        : String(value ?? '')
  return (
    <input
      {...shared}
      type={inputType}
      inputMode={numeric ? 'decimal' : undefined}
      value={displayed}
      onChange={(event) => onChange(event.target.value)}
    />
  )
}
export function RecordForm({
  collection,
  record,
  onSaved,
  onCancel,
}: {
  collection: Collection
  record?: RecordRow
  onSaved: (record: RecordRow) => void
  onCancel: () => void
}) {
  const { t, i18n } = useTranslation()
  const [initialValues] = useState<Record<string, unknown>>(() => ({
    ...Object.fromEntries(
      collection.fields
        .filter((field) => field.type === 'boolean')
        .map((field) => [field.id, false]),
    ),
    ...record?.values,
  }))
  const [values, setValues] = useState(initialValues)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  const [saved, setSaved] = useState(false)
  const [uploads, setUploads] = useState<Record<string, boolean>>({})
  const uploading = Object.values(uploads).some(Boolean)
  const dirty = !saved && JSON.stringify(values) !== JSON.stringify(initialValues)
  const blocker = useBlocker({
    shouldBlockFn: () => (dirty || uploading) && !busy,
    enableBeforeUnload: dirty || uploading,
    withResolver: true,
  })
  return (
    <form
      className="data-form"
      onSubmit={async (event) => {
        event.preventDefault()
        if (uploading) return
        const converted: Record<string, unknown> = {}
        const nextErrors: Record<string, string> = {}
        for (const field of collection.fields) {
          const raw = values[field.id]
          if (field.required && (raw == null || raw === '' || (Array.isArray(raw) && !raw.length)))
            nextErrors[field.id] = t('dataRequired')
          try {
            converted[field.id] = fieldValue(field, raw, i18n.language)
          } catch {
            nextErrors[field.id] = t('dataSaveError')
          }
          const value = converted[field.id]
          if (typeof value === 'number') {
            if (!Number.isFinite(value)) nextErrors[field.id] = t('dataInvalidNumber')
            else if (field.type === 'integer' && !Number.isInteger(value))
              nextErrors[field.id] = t('dataInvalidInteger')
            else if (
              (field.min !== undefined && value < field.min) ||
              (field.max !== undefined && value > field.max)
            )
              nextErrors[field.id] = t('dataOutOfRange')
          }
        }
        setErrors(nextErrors)
        if (Object.keys(nextErrors).length) {
          document.getElementById(`field-${Object.keys(nextErrors)[0]}`)?.focus()
          return
        }
        setBusy(true)
        setFailed(false)
        try {
          const result = record
            ? await rpc.call('records.update', {
                collection: collection.id,
                id: record.id,
                values: converted,
              })
            : await rpc.call('records.create', { collection: collection.id, values: converted })
          setSaved(true)
          onSaved(result.record)
        } catch (error) {
          setErrors(validationErrors(error))
          setFailed(true)
        } finally {
          setBusy(false)
        }
      }}
    >
      <fieldset disabled={busy}>
        <div className="data-form-grid">
          {collection.fields.map((field) => (
            <div
              className={`data-form-field ${field.type === 'long_text' ? 'data-wide' : ''}`}
              key={field.id}
            >
              <label htmlFor={`field-${field.id}`}>
                {field.name}
                {field.required && <span aria-hidden="true"> *</span>}
                {field.type === 'money' && field.currency && <small> ({field.currency})</small>}
              </label>
              {field.description && <p>{field.description}</p>}
              <FieldInput
                field={field}
                value={values[field.id]}
                onChange={(value) => {
                  setValues((previous) => ({ ...previous, [field.id]: value }))
                  setErrors((previous) => ({ ...previous, [field.id]: '' }))
                }}
                onBusy={(value) => setUploads((previous) => ({ ...previous, [field.id]: value }))}
                invalid={!!errors[field.id]}
              />
              {errors[field.id] && (
                <p id={`error-${field.id}`} role="alert">
                  {errors[field.id]}
                </p>
              )}
            </div>
          ))}
        </div>
        <div className="data-actions">
          <Button variant="primary" type="submit" disabled={busy || uploading}>
            {t('dataSave')}
          </Button>
          <Button onClick={onCancel}>{t('dataCancel')}</Button>
        </div>
      </fieldset>
      {failed && <p role="alert">{t('dataSaveError')}</p>}
      {blocker.status === 'blocked' && (
        <Dialog title="dataDiscard" onClose={() => blocker.reset()} footer={false}>
          <p>{t('dataUnsaved')}</p>
          <div className="data-actions">
            <Button variant="destructive" onClick={() => blocker.proceed()}>
              {t('dataDiscard')}
            </Button>
            <Button onClick={() => blocker.reset()}>{t('dataCancel')}</Button>
          </div>
        </Dialog>
      )}
    </form>
  )
}
