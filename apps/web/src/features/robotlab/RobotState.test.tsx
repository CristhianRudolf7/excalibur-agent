import { beforeEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import i18n from '@/lib/i18n'
import { RobotState } from './RobotState'

beforeEach(async () => {
  cleanup()
  await i18n.changeLanguage('en')
})
it.each(['en', 'es'])('shows compact dog support and trunk measurements (%s)', async (language) => {
  await i18n.changeLanguage(language)
  render(
    <RobotState
      robot="dog"
      state={{
        support: ['FL', 'FR', 'RL'],
        margin: -0.012,
        z: 0.2781,
        pitch_deg: 2.31,
        roll_deg: -1.29,
      }}
      time={3.2}
    />,
  )
  expect(
    screen.getByRole('region', {
      name: language === 'es' ? 'Último estado del robot' : 'Latest robot state',
    }),
  ).toBeVisible()
  expect(screen.getByText('FL · FR · RL')).toBeVisible()
  expect(screen.getByText(language === 'es' ? '-0,012 m' : '-0.012 m')).toBeVisible()
  expect(screen.getByText(language === 'es' ? '0,278 m' : '0.278 m')).toBeVisible()
  expect(screen.getByText(language === 'es' ? '2,3°' : '2.3°')).toBeVisible()
  expect(screen.getByText(language === 'es' ? '-1,3°' : '-1.3°')).toBeVisible()
})
it.each(['en', 'es'])('shows gripper pose, opening and held object (%s)', async (language) => {
  await i18n.changeLanguage(language)
  render(
    <RobotState
      robot="arm"
      state={{
        gripper: {
          pos: [0.1, 0.2, 0.9],
          yaw_deg: 20,
          pitch_deg: 0,
          roll_deg: 0,
          opening: 0.5,
          opening_m: 0.0425,
        },
        held: 'cube',
      }}
    />,
  )
  expect(screen.getByText('cube')).toBeVisible()
  expect(
    screen.getByText(language === 'es' ? '0,100 / 0,200 / 0,900 m' : '0.100 / 0.200 / 0.900 m'),
  ).toBeVisible()
  expect(screen.getByText(language === 'es' ? /50.*0,043 m/ : /50.*0.043 m/)).toBeVisible()
})
it('handles no state, no support, no held object and malformed numbers', () => {
  const view = render(<RobotState robot="dog" />)
  expect(screen.getByText('No robot state recorded yet.')).toBeVisible()
  view.rerender(<RobotState robot="dog" state={{ support: [], margin: NaN }} />)
  expect(screen.getByText('None')).toBeVisible()
  expect(view.container.textContent).not.toContain('NaN')
  view.rerender(<RobotState robot="arm" state={{ gripper: {}, held: null }} />)
  expect(screen.getByText('None')).toBeVisible()
})
