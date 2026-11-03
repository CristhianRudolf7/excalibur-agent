import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
vi.mock('../teleop/i18n', () => ({ useRobotText: () => (key: string) => key }))
import { DecisionCard } from './VisionPanel'
it('reports simulation and model disagreement separately', () => {
  render(
    <DecisionCard
      step={{
        session: 's',
        step_id: 'v',
        phase: 'done',
        decision: {
          objects: [],
          action: { mark: 1, target: 'A' },
          explanation: 'Placed',
          verified: true,
          model_verified: false,
          verify_note: 'simulation',
        },
      }}
    />,
  )
  expect(screen.getByText('simulationVerified: ✓')).toBeVisible()
  expect(screen.getByText('modelOpinion: ✗')).toBeVisible()
})
it('keeps model-only verification available', () => {
  render(
    <DecisionCard
      step={{
        session: 's',
        step_id: 'v',
        phase: 'done',
        decision: {
          objects: [],
          action: null,
          explanation: 'Placed',
          verified: false,
          verify_note: 'model',
        },
      }}
    />,
  )
  expect(screen.getByText('verify: ✗')).toBeVisible()
  expect(screen.queryByText(/simulationVerified/)).not.toBeInTheDocument()
})
