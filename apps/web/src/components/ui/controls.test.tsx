import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { Select } from './select'
import { Segmented } from './segmented'
import { Button } from './button'

const options = [
  { value: 'first', label: 'First' },
  { value: 'disabled', label: 'Unavailable', disabled: true },
  { value: 'last', label: 'Last' },
]
afterEach(cleanup)

it('buttons do not submit forms unless explicitly requested', async () => {
  const submit = vi.fn((event) => event.preventDefault())
  render(
    <form onSubmit={submit}>
      <Button>Open</Button>
      <Button type="submit">Save</Button>
    </form>,
  )
  const user = userEvent.setup()
  await user.click(screen.getByText('Open'))
  expect(submit).not.toHaveBeenCalled()
  await user.click(screen.getByText('Save'))
  expect(submit).toHaveBeenCalledOnce()
})

it('segmented selection supports arrow keys, skips disabled items and stays controlled', async () => {
  function Example() {
    const [value, setValue] = useState('first')
    return <Segmented label="View" value={value} onValueChange={setValue} options={options} />
  }
  render(<Example />)
  const user = userEvent.setup()
  await user.tab()
  expect(screen.getByRole('radio', { name: 'First' })).toHaveFocus()
  await user.keyboard('{ArrowRight>}')
  await waitFor(() =>
    expect(screen.getByRole('radio', { name: 'Last' })).toHaveAttribute('aria-checked', 'true'),
  )
  await user.keyboard('{/ArrowRight}')
  expect(screen.getByRole('radio', { name: 'Unavailable' })).toBeDisabled()
})

it('select opens with keyboard, skips disabled items, commits and restores focus', async () => {
  // Radix scrolls the highlighted option into view; jsdom has no layout engine.
  Element.prototype.scrollIntoView = vi.fn()
  function Example() {
    const [value, setValue] = useState('first')
    return <Select label="Order" value={value} onValueChange={setValue} options={options} />
  }
  render(<Example />)
  const user = userEvent.setup()
  await user.tab()
  await user.keyboard('{ArrowDown}')
  expect(screen.getByRole('listbox')).toBeVisible()
  await user.keyboard('{End}{Enter}')
  expect(screen.getByRole('combobox', { name: 'Order' })).toHaveTextContent('Last')
  expect(screen.getByRole('combobox', { name: 'Order' })).toHaveFocus()
  await user.keyboard('{Enter}{Escape}')
  expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
})

it('select keeps options inside a surrounding native dialog top layer', async () => {
  render(
    <dialog open aria-label="Preferences">
      <Select label="Order" value="first" onValueChange={vi.fn()} options={options} />
    </dialog>,
  )
  const user = userEvent.setup()
  screen.getByRole('combobox').focus()
  await user.keyboard('{ArrowDown}')
  expect(screen.getByRole('listbox').closest('dialog')).toBe(screen.getByRole('dialog'))
  await user.keyboard('{Escape}')
  expect(screen.getByRole('combobox')).toHaveFocus()
})
