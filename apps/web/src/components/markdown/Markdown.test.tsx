import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@/lib/i18n'
import MarkdownBody from './MarkdownBody'
import { CodeBlock } from './CodeBlock'
import { safeMarkdownUrl } from './urls'

const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
  if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
  else Reflect.deleteProperty(navigator, 'clipboard')
})
it('renders headings, emphasis, nested lists, breaks, quotes and horizontal rules', () => {
  const { container } = render(
    <MarkdownBody>
      {
        '# One\n\n## Two\n\n### Three\n\n#### Four\n\n**Bold** and *italic* and ~~removed~~ and `inline`  \nnext line\n\n- Parent\n  - Child\n\n1. First\n2. Second\n\n> **Note:** quoted\n\n---'
      }
    </MarkdownBody>,
  )
  for (let level = 1; level <= 4; level++)
    expect(screen.getByRole('heading', { level })).toBeInTheDocument()
  expect(container.querySelector('strong')).toHaveTextContent('Bold')
  expect(container.querySelector('em')).toHaveTextContent('italic')
  expect(container.querySelector('del')).toHaveTextContent('removed')
  expect(container.querySelector('code')).toHaveTextContent('inline')
  expect(container.querySelectorAll('br')).toHaveLength(1)
  expect(container.querySelector('ul ul')).toHaveTextContent('Child')
  expect(container.querySelector('ol')).toHaveTextContent('First')
  expect(container.querySelector('blockquote')).toHaveTextContent('quoted')
  expect(container.querySelector('hr')).not.toBeNull()
})
it('renders GFM tables with alignment and read-only task list checkboxes', () => {
  const { container } = render(
    <MarkdownBody>
      {'| Item | Amount |\n| :--- | ---: |\n| A | 12 |\n\n- [x] Complete\n- [ ] Pending'}
    </MarkdownBody>,
  )
  expect(screen.getByRole('region', { name: 'Tabla del mensaje' })).toContainElement(
    screen.getByRole('table'),
  )
  expect(screen.getByRole('columnheader', { name: 'Amount' })).toHaveStyle({ textAlign: 'right' })
  const tasks = screen.getAllByRole('checkbox')
  expect(tasks[0]).toBeChecked()
  expect(tasks[1]).not.toBeChecked()
  for (const task of tasks) expect(task).toBeDisabled()
  expect(container.querySelector('tbody td:last-child')).toHaveStyle({ textAlign: 'right' })
})
it('opens safe links in a new tab and lazy loads bounded images', () => {
  render(
    <MarkdownBody>
      {
        '[Docs](https://example.com/docs) and [Mail](mailto:hello@example.com)\n\n![Preview](https://example.com/image.png)'
      }
    </MarkdownBody>,
  )
  for (const link of screen.getAllByRole('link')) {
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(link.querySelector('svg')).not.toBeNull()
  }
  expect(screen.getByRole('img', { name: 'Preview' })).toHaveAttribute('loading', 'lazy')
})
it('escapes raw HTML and removes executable or unsupported URLs', () => {
  const html =
    '<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[Bad](javascript:alert(1))\n\n[Data](data:text/html,unsafe)\n\n[Relative](/private)'
  const { container } = render(<MarkdownBody>{html}</MarkdownBody>)
  expect(container.querySelector('script')).toBeNull()
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('a')).toBeNull()
  expect(container).toHaveTextContent('<script>alert(1)</script>')
})
it.each([
  'javascript:alert(1)',
  'java\nscript:alert(1)',
  'data:text/html,test',
  'vbscript:test',
  'file:///etc/passwd',
  '//example.com/file',
  '/relative',
])('rejects unsafe URL %s', (url) => {
  expect(safeMarkdownUrl(url)).toBe('')
})
it('accepts only approved protocols and disallows mail images', () => {
  expect(safeMarkdownUrl('HTTP://example.com')).toBe('http://example.com/')
  expect(safeMarkdownUrl('mailto:hello@example.com')).toBe('mailto:hello@example.com')
  expect(safeMarkdownUrl('mailto:hello@example.com', 'src')).toBe('')
})
it('copies code exactly and restores the label after 1.5 seconds', async () => {
  vi.useFakeTimers()
  const copy = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
  render(<CodeBlock code={'const x = 1\nconsole.log(x)'} language="typescript" />)
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Copiar' }))
  })
  expect(copy).toHaveBeenCalledWith('const x = 1\nconsole.log(x)')
  expect(screen.getByRole('button', { name: 'Copiado' })).toBeInTheDocument()
  act(() => vi.advanceTimersByTime(1500))
  expect(screen.getByRole('button', { name: 'Copiar' })).toBeInTheDocument()
})
it('handles unfinished streamed fences and tables without remounting the code toolbar', () => {
  const view = render(<MarkdownBody>{'```typescript\nconst x = 1'}</MarkdownBody>)
  const button = screen.getByRole('button', { name: 'Copiar' })
  expect(screen.getByLabelText('Código typescript')).toHaveTextContent('const x = 1')
  view.rerender(<MarkdownBody>{'```typescript\nconst x = 123\n```'}</MarkdownBody>)
  expect(screen.getByRole('button', { name: 'Copiar' })).toBe(button)
  view.rerender(<MarkdownBody>{'| A | B |\n| --- | --- |\n| 1 |'}</MarkdownBody>)
  expect(screen.getByRole('table')).toBeInTheDocument()
})
