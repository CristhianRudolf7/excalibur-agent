import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import i18n from '@/lib/i18n'
import { Tabs } from './Tabs'
beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
})
it('exposes the selected tab and supports arrow/Home/End focus', () => {
  const change = vi.fn()
  render(
    <Tabs
      label="Robot lab"
      values={['environments', 'trainings']}
      value="environments"
      onChange={change}
    />,
  )
  const environments = screen.getByRole('tab', { name: 'Environments' })
  const trainings = screen.getByRole('tab', { name: 'Trainings' })
  expect(environments).toHaveAttribute('aria-selected', 'true')
  expect(trainings).toHaveAttribute('tabindex', '-1')
  environments.focus()
  fireEvent.keyDown(environments, { key: 'ArrowRight' })
  expect(trainings).toHaveFocus()
  expect(change).toHaveBeenLastCalledWith('trainings')
  fireEvent.keyDown(trainings, { key: 'Home' })
  expect(environments).toHaveFocus()
  expect(change).toHaveBeenLastCalledWith('environments')
})
it('renders the Spanish navigation labels', async () => {
  await i18n.changeLanguage('es')
  render(
    <Tabs
      label="Laboratorio robótico"
      values={['environments', 'trainings']}
      value="trainings"
      onChange={vi.fn()}
    />,
  )
  expect(screen.getByRole('tab', { name: 'Ambientes' })).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Entrenamientos' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})
