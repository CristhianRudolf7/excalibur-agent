import type { Field, Filter } from '@/lib/rpc/contract'
import { RpcError } from '@/lib/rpc/client'

export function parseNumber(value: string, locale: string) {
  const parts = new Intl.NumberFormat(locale).formatToParts(12345.6)
  const group = parts.find((p) => p.type === 'group')?.value ?? ','
  const decimal = parts.find((p) => p.type === 'decimal')?.value ?? '.'
  let normalized = value.trim().replace(/[\s\u00a0]/g, '')
  // A lone comma is a decimal separator in either locale; grouped values follow the locale.
  if (normalized.includes(',') && !normalized.includes('.') && decimal === '.')
    normalized = normalized.replace(',', '.')
  else if (decimal === ',' && /^[-+]?\d+\.\d{1,2}$/.test(normalized)) {
    /* Accept an unambiguous dot decimal too. */
  } else normalized = normalized.split(group).join('').replace(decimal, '.')
  return /^[+-]?(?:\d+\.?\d*|\.\d+)(?:e[+-]?\d+)?$/i.test(normalized) ? Number(normalized) : NaN
}
export function fieldValue(field: Field, value: unknown, locale: string): unknown {
  if (value === '' || value === undefined || value === null) return null
  if (['number', 'money', 'integer'].includes(field.type))
    return typeof value === 'number' ? value : parseNumber(String(value), locale)
  if (field.type === 'datetime' && typeof value === 'string') return new Date(value).toISOString()
  return value
}
export function validationErrors(error: unknown): Record<string, string> {
  if (
    !(error instanceof RpcError) ||
    error.code !== -32602 ||
    !error.data ||
    typeof error.data !== 'object' ||
    !('errors' in error.data) ||
    !Array.isArray(error.data.errors)
  )
    return {}
  return Object.fromEntries(
    error.data.errors.flatMap((entry: unknown) =>
      entry &&
      typeof entry === 'object' &&
      'field' in entry &&
      'message' in entry &&
      typeof entry.field === 'string' &&
      typeof entry.message === 'string'
        ? [[entry.field, entry.message]]
        : [],
    ),
  )
}
export function formatValue(field: Field, value: unknown, locale: string, timezone?: string) {
  if (value === null || value === undefined || value === '') return '—'
  if (['money', 'number', 'integer'].includes(field.type) && typeof value === 'number') {
    try {
      return new Intl.NumberFormat(
        locale,
        field.type === 'money' ? { style: 'currency', currency: field.currency || 'USD' } : {},
      ).format(value)
    } catch {
      return String(value)
    }
  }
  if (field.type === 'date' || field.type === 'datetime') {
    const date = new Date(
      field.type === 'date' ? `${String(value).slice(0, 10)}T12:00:00Z` : String(value),
    )
    if (Number.isNaN(date.getTime())) return String(value)
    return new Intl.DateTimeFormat(locale, {
      dateStyle: 'medium',
      ...(field.type === 'datetime'
        ? { timeStyle: 'short', timeZone: timezone }
        : { timeZone: 'UTC' }),
    }).format(date)
  }
  return Array.isArray(value) ? value.join(', ') : String(value)
}
export function operators(field: Field): Filter['op'][] {
  const empty: Filter['op'][] = ['is_empty', 'not_empty']
  if (['number', 'integer', 'money', 'date', 'datetime'].includes(field.type))
    return ['eq', 'neq', 'lt', 'lte', 'gt', 'gte', ...empty]
  if (['boolean', 'relation', 'select', 'file'].includes(field.type)) return ['eq', 'neq', ...empty]
  return ['eq', 'neq', 'contains', 'in', ...empty]
}
export function localDateTime(value: unknown) {
  if (!value) return ''
  const date = new Date(String(value))
  if (Number.isNaN(date.getTime())) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}
