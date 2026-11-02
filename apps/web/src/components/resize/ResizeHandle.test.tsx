import { useState } from 'react'
import { afterEach, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { ResizeHandle } from './ResizeHandle'

afterEach(cleanup)
function Pane({ direction = 1 }: { direction?: 1 | -1 }) {
  const [value, change] = useState(240)
  return (
    <ResizeHandle
      label="Size"
      min={200}
      max={400}
      defaultValue={240}
      value={value}
      onChange={change}
      getPointerValue={(x) => x}
      direction={direction}
    />
  )
}
it('shares accessible keyboard boundaries and a default reset across panes', () => {
  render(<Pane />)
  const handle = screen.getByRole('separator', { name: 'Size' })
  expect(handle).toHaveAttribute('aria-orientation', 'vertical')
  fireEvent.keyDown(handle, { key: 'ArrowRight' })
  expect(handle).toHaveAttribute('aria-valuenow', '264')
  fireEvent.keyDown(handle, { key: 'Home' })
  fireEvent.keyDown(handle, { key: 'ArrowLeft' })
  expect(handle).toHaveAttribute('aria-valuenow', '200')
  fireEvent.keyDown(handle, { key: 'End' })
  fireEvent.keyDown(handle, { key: 'ArrowRight' })
  expect(handle).toHaveAttribute('aria-valuenow', '400')
  fireEvent.doubleClick(handle)
  expect(handle).toHaveAttribute('aria-valuenow', '240')
})
it('reverses keyboard direction for a panel anchored to the right', () => {
  render(<Pane direction={-1} />)
  const handle = screen.getByRole('separator')
  fireEvent.keyDown(handle, { key: 'ArrowLeft' })
  expect(handle).toHaveAttribute('aria-valuenow', '264')
})
