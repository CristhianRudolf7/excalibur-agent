import { afterEach, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import '@/lib/i18n'
import { Categories } from './Categories'

afterEach(cleanup)
it('keeps only the largest eight categories visible and reflects a selected overflow category', () => {
  const groups = Array.from({ length: 31 }, (_, index) => ({
    id: `group-${index}`,
    name_en: `Category ${index}`,
    name_es: `Categoría ${index}`,
    description_en: '',
    description_es: '',
    count: index + 1,
  }))
  const changes: string[] = []
  const { container } = render(
    <Categories
      groups={groups}
      total={496}
      value="group-0"
      onChange={(value) => changes.push(value)}
    />,
  )
  const row = container.querySelector('.skill-categories')!
  expect(within(row as HTMLElement).getAllByRole('button')).toHaveLength(9)
  expect(within(row as HTMLElement).getAllByRole('button')[1]).toHaveTextContent('Categoría 30')
  expect(screen.getByRole('button', { name: /^Categoría 0$/ })).toHaveAttribute(
    'data-selected',
    'true',
  )
  fireEvent.click(screen.getByRole('button', { name: 'Todos 496' }))
  expect(changes).toEqual([''])
  // Sorting must not reorder the server's category array.
  expect(groups[0]!.id).toBe('group-0')
})
