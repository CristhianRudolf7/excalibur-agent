import { CollectionSummary } from './MyData'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import '@/lib/i18n'
import { rpc, RpcError } from '@/lib/rpc/client'
import { FieldInput, RecordForm } from './RecordForm'
import { CollectionNav } from './CollectionNav'
import { DeleteRecord } from './DeleteRecord'
import { RecordList } from './RecordList'
import { changeList, initialListState } from './list-state'
import { fieldValue, formatValue, operators, parseNumber, validationErrors } from './values'
import { exampleCollection as collection, exampleRecords } from './fixtures'
import type { FieldType } from '@/lib/rpc/contract'

const navigate = vi.fn()
vi.mock('@tanstack/react-router', async (original) => ({
  ...(await original<object>()),
  useNavigate: () => navigate,
  useBlocker: () => ({ status: 'idle' }),
  Link: ({
    children,
    to,
    params: _params,
    ...props
  }: {
    children: ReactNode
    to: string
    params?: unknown
  }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}))
beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = () => {}
  vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    const result =
      method === 'principal.get'
        ? { id: 'local', timezone: 'America/Lima' }
        : method === 'collections.get'
          ? { collection }
          : method === 'records.list'
            ? { records: exampleRecords, total: 30, next_cursor: 'next' }
            : method === 'records.get'
              ? { record: exampleRecords[0], related: [] }
              : method === 'collections.summary'
                ? { cards: [] }
                : { ok: true }
    return result as never
  })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}>{children}</QueryClientProvider>)
}
const types: FieldType[] = [
  'text',
  'long_text',
  'number',
  'integer',
  'money',
  'date',
  'datetime',
  'boolean',
  'select',
  'multi_select',
  'relation',
  'email',
  'url',
  'file',
]
it.each(types)('generates the %s input with its schema label', async (type) => {
  const field = collection.fields.find((item) => item.type === type)!
  const onChange = vi.fn()
  const { container } = wrap(
    <>
      <label htmlFor={`field-${field.id}`}>{field.name}</label>
      <FieldInput field={field} value={type === 'boolean' ? false : ''} onChange={onChange} />
    </>,
  )
  expect(container.querySelector(`#field-${field.id}`)).toBeInTheDocument()
  if (type === 'multi_select') {
    fireEvent.click(screen.getByLabelText('Core'))
    expect(onChange).toHaveBeenCalledWith(['Core'])
  }
  if (type === 'relation')
    await waitFor(() =>
      expect(screen.getByRole('option', { name: 'Mathematics' })).toBeInTheDocument(),
    )
  if (type === 'boolean') {
    fireEvent.click(screen.getByLabelText(field.name))
    expect(onChange).toHaveBeenCalledWith(true)
  }
  if (type === 'long_text') expect(container.querySelector('textarea')).toBeInTheDocument()
  if (type === 'file') expect(container.querySelector('input')).toHaveAttribute('type', 'file')
  if (['number', 'money', 'integer'].includes(type))
    expect(container.querySelector('input')).toHaveAttribute('inputmode', 'decimal')
})
it('parses localized decimals, preserves false, formats currency and date-only values', () => {
  expect(parseNumber('15,5', 'es')).toBe(15.5)
  expect(parseNumber('1.234,50', 'es')).toBe(1234.5)
  expect(parseNumber('1,234.50', 'en')).toBe(1234.5)
  expect(fieldValue(collection.fields[6]!, false, 'es')).toBe(false)
  expect(fieldValue(collection.fields[1]!, '', 'es')).toBeNull()
  expect(formatValue(collection.fields[3]!, 500.5, 'en')).toContain('500.50')
  expect(formatValue(collection.fields[4]!, '2026-11-10', 'en', 'Pacific/Honolulu')).toContain('10')
  expect(operators(collection.fields[1]!)).toContain('gte')
  expect(operators(collection.fields[6]!)).not.toContain('contains')
})
it('extracts field errors only from structured validation responses', () => {
  expect(
    validationErrors(
      new RpcError(-32602, 'Invalid', { errors: [{ field: 'course', message: 'Already exists' }] }),
    ),
  ).toEqual({ course: 'Already exists' })
  expect(validationErrors(new Error('offline'))).toEqual({})
})
it('shows server validation next to the failing input and preserves user input', async () => {
  vi.mocked(rpc.call).mockRejectedValue(
    new RpcError(-32602, 'Invalid', { errors: [{ field: 'course', message: 'Already exists' }] }),
  )
  const small = { ...collection, fields: [collection.fields[0]!] }
  const { container } = wrap(<RecordForm collection={small} onSaved={vi.fn()} onCancel={vi.fn()} />)
  fireEvent.change(screen.getByLabelText(/Course/), { target: { value: 'Physics' } })
  fireEvent.submit(container.querySelector('form')!)
  expect(await screen.findByText('Already exists')).toBeInTheDocument()
  expect(screen.getByLabelText(/Course/)).toHaveAttribute('aria-invalid', 'true')
  expect(screen.getByLabelText(/Course/)).toHaveValue('Physics')
})
it('validates required, integer and range constraints before sending', async () => {
  const { container } = wrap(
    <RecordForm
      collection={{ ...collection, fields: collection.fields.slice(0, 3) }}
      onSaved={vi.fn()}
      onCancel={vi.fn()}
    />,
  )
  fireEvent.change(screen.getByLabelText('Grade'), { target: { value: '25' } })
  fireEvent.change(screen.getByLabelText('Credits'), { target: { value: '2,5' } })
  fireEvent.submit(container.querySelector('form')!)
  expect(await screen.findByText('Este campo es obligatorio.')).toBeInTheDocument()
  expect(screen.getByText('Introduce un número entero.')).toBeInTheDocument()
  expect(screen.getByText('El valor está fuera del rango permitido.')).toBeInTheDocument()
  expect(rpc.call).not.toHaveBeenCalledWith('records.create', expect.anything())
})
it('resets cursor history on filter, sort or search changes', () => {
  const paged = { ...initialListState, cursors: [undefined, 'page-two'] }
  expect(changeList(paged, { query: 'math' }).cursors).toEqual([undefined])
  expect(changeList(paged, { sort: [{ field: 'grade', dir: 'desc' }] }).sort[0]?.dir).toBe('desc')
  expect(
    changeList(paged, { filters: [{ field: 'grade', op: 'gte', value: 15 }] }).filters,
  ).toHaveLength(1)
})
it('sends filters, sort and cursor pagination through the shared RPC', async () => {
  wrap(<RecordList collection={{ ...collection, summary: [] }} />)
  await screen.findByText('30 registros')
  fireEvent.click(screen.getByRole('button', { name: 'Siguiente' }))
  await waitFor(() =>
    expect(rpc.call).toHaveBeenCalledWith(
      'records.list',
      expect.objectContaining({ cursor: 'next' }),
      expect.anything(),
    ),
  )
  fireEvent.click(screen.getByRole('button', { name: 'Ordenar por Grade' }))
  await waitFor(() =>
    expect(rpc.call).toHaveBeenCalledWith(
      'records.list',
      expect.objectContaining({ cursor: undefined, sort: [{ field: 'grade', dir: 'asc' }] }),
      expect.anything(),
    ),
  )
  fireEvent.click(screen.getByRole('button', { name: 'Añadir filtro' }))
  fireEvent.change(screen.getByLabelText('Campo'), { target: { value: 'grade' } })
  fireEvent.change(screen.getByLabelText('Operador'), { target: { value: 'gte' } })
  fireEvent.change(screen.getByLabelText('Valor'), { target: { value: '15,5' } })
  await waitFor(() =>
    expect(rpc.call).toHaveBeenCalledWith(
      'records.list',
      expect.objectContaining({ filters: [{ field: 'grade', op: 'gte', value: 15.5 }] }),
      expect.anything(),
    ),
  )
})
it('requires explicit delete confirmation and displays reference warning', async () => {
  const deleted = vi.fn()
  wrap(
    <DeleteRecord
      collection={collection}
      record={exampleRecords[0]!}
      referenced
      onClose={vi.fn()}
      onDeleted={deleted}
    />,
  )
  expect(screen.getByRole('dialog')).toHaveTextContent('Mathematics')
  expect(screen.getByRole('note')).toHaveTextContent('referencia')
  expect(rpc.call).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Eliminar registro' }))
  await waitFor(() => expect(deleted).toHaveBeenCalledOnce())
  expect(rpc.call).toHaveBeenCalledWith('records.delete', { collection: 'grades', id: 'math' })
})
it('lists collection badges, collapses children and offers agent creation when empty', () => {
  const go = vi.fn(),
    ask = vi.fn()
  const view = render(
    <CollectionNav collections={[collection]} active="/data/grades" onNavigate={go} onAsk={ask} />,
  )
  fireEvent.click(screen.getByRole('button', { name: /University grades/ }))
  expect(go).toHaveBeenCalledWith('data:grades')
  fireEvent.click(screen.getByRole('button', { name: 'Ocultar colecciones' }))
  expect(screen.queryByText('University grades')).toBeNull()
  view.rerender(<CollectionNav collections={[]} active="/data" onNavigate={go} onAsk={ask} />)
  fireEvent.click(screen.getByRole('button', { name: 'Mostrar colecciones' }))
  fireEvent.click(screen.getByRole('button', { name: 'Pídele al agente que cree una colección' }))
  expect(ask).toHaveBeenCalledOnce()
})

it('keeps localized decimal typing intact across successive filter keystrokes', async () => {
  wrap(<RecordList collection={{ ...collection, summary: [] }} />)
  await screen.findByText('30 registros')
  fireEvent.click(screen.getByRole('button', { name: 'Añadir filtro' }))
  fireEvent.change(screen.getByLabelText('Campo'), { target: { value: 'grade' } })
  const input = screen.getByLabelText('Valor')
  for (const value of ['1', '15', '15,', '15,5']) {
    fireEvent.change(input, { target: { value } })
    expect(input).toHaveValue(value)
  }
  await waitFor(() =>
    expect(rpc.call).toHaveBeenCalledWith(
      'records.list',
      expect.objectContaining({ filters: [{ field: 'grade', op: 'eq', value: 15.5 }] }),
      expect.anything(),
    ),
  )
})
it('retains the record and offers retry after delete fails', async () => {
  vi.mocked(rpc.call).mockRejectedValueOnce(new Error('offline'))
  const deleted = vi.fn()
  wrap(
    <DeleteRecord
      collection={collection}
      record={exampleRecords[0]!}
      referenced={false}
      onClose={vi.fn()}
      onDeleted={deleted}
    />,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Eliminar registro' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('No se pudo completar')
  expect(deleted).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Eliminar registro' })).toBeEnabled()
})
it('submits false for an unchecked required boolean', async () => {
  const saved = vi.fn()
  vi.mocked(rpc.call).mockResolvedValue({ record: exampleRecords[0] } as never)
  const { container } = wrap(
    <RecordForm
      collection={{
        ...collection,
        fields: [{ id: 'passed', name: 'Passed', type: 'boolean', required: true }],
      }}
      onSaved={saved}
      onCancel={vi.fn()}
    />,
  )
  fireEvent.submit(container.querySelector('form')!)
  await waitFor(() => expect(saved).toHaveBeenCalledOnce())
  expect(rpc.call).toHaveBeenCalledWith('records.create', {
    collection: 'grades',
    values: { passed: false },
  })
})

it.each(['bar', 'line', 'pie'] as const)(
  'renders an accessible %s summary chart including negative values',
  async (chart) => {
    vi.mocked(rpc.call).mockResolvedValue({
      cards: [
        {
          id: 'avg',
          title: 'Net income',
          value: -10,
          series: [
            { label: 'Income', value: 20 },
            { label: 'Expenses', value: -30 },
          ],
        },
      ],
    } as never)
    const { container } = wrap(
      <CollectionSummary
        collection={{
          ...collection,
          summary: [{ id: 'avg', title: 'Net income', kind: 'sum', field: 'tuition', chart }],
        }}
      />,
    )
    expect(
      await screen.findByRole('img', { name: 'Income: 20, Expenses: -30' }),
    ).toBeInTheDocument()
    expect(screen.getByText('Expenses: -30')).toBeInTheDocument()
    for (const rect of container.querySelectorAll('rect')) {
      expect(Number(rect.getAttribute('y'))).toBeGreaterThanOrEqual(5)
      expect(
        Number(rect.getAttribute('y')) + Number(rect.getAttribute('height')),
      ).toBeLessThanOrEqual(55)
    }
  },
)
