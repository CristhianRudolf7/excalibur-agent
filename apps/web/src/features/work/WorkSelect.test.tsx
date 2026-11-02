import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { WorkSelect } from './WorkSelect'

const scrollDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')

afterEach(() => {
  if (scrollDescriptor) Object.defineProperty(Element.prototype, 'scrollIntoView', scrollDescriptor)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('round-trips empty and reserved-looking values without changing project identifiers', async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
  const change = vi.fn()
  const options = [
    { value: '', label: 'All' },
    { value: 'value:project', label: 'Compiler' },
  ]
  const view = render(<WorkSelect label="Project" value="" onChange={change} options={options} />)
  const trigger = screen.getByRole('combobox', { name: 'Project' })
  expect(trigger).toHaveTextContent('All')
  fireEvent.keyDown(trigger, { key: 'Enter' })
  fireEvent.click(await screen.findByRole('option', { name: 'Compiler' }))
  expect(change).toHaveBeenLastCalledWith('value:project')
  view.rerender(
    <WorkSelect label="Project" value="value:project" onChange={change} options={options} />,
  )
  expect(trigger).toHaveTextContent('Compiler')
  fireEvent.keyDown(trigger, { key: 'Enter' })
  fireEvent.click(await screen.findByRole('option', { name: 'All' }))
  expect(change).toHaveBeenLastCalledWith('')
})
