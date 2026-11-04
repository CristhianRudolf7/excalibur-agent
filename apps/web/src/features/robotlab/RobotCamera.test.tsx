import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { RobotCamera } from './RobotCamera'
import type { Session } from './types'
import { frameFixture } from './viewer/fixtures.test-support'

const call = vi.hoisted(() => vi.fn())
vi.mock('./api', () => ({ labCall: call }))
vi.mock('./viewer', () => ({
  SceneView: ({ label }: { label: string }) => <div role="img" aria-label={label} />,
}))
beforeEach(async () => {
  cleanup()
  call.mockReset().mockResolvedValue({ png: 'alternate-camera' })
  await i18n.changeLanguage('en')
})
function mount(robot: Session['robot'], live = true) {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <RobotCamera
        session={{ id: 'test', robot, attempt: 1, status: live ? 'live' : 'stopped' } as Session}
        scene={null}
        loading={false}
        frame={{
          ...frameFixture(0),
          camera: {
            name: robot === 'dog' ? 'robot/head' : 'robot/wrist',
            pose: [0, 0, 0, 1, 0, 0, 0],
          },
        }}
      />
    </QueryClientProvider>,
  )
}
it.each(['dog', 'arm'] as const)(
  'offers only the %s cameras and looks through the alternate camera without motion',
  async (robot) => {
    mount(robot)
    const select = screen.getByLabelText('Camera') as HTMLSelectElement
    const names = robot === 'dog' ? ['head', 'rear'] : ['wrist', 'overhead']
    expect(Array.from(select.options).map((option) => option.value)).toEqual(names)
    expect(call).not.toHaveBeenCalled()
    fireEvent.change(select, { target: { value: names[1] } })
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith(
        'robotlab.robot.look',
        { session: 'test', camera: `robot/${names[1]}` },
        expect.anything(),
      ),
    )
    expect(await screen.findByRole('img', { name: 'Robot camera' })).toHaveAttribute(
      'src',
      'data:image/png;base64,alternate-camera',
    )
  },
)
it('does not query a stopped world when an alternate camera was not recorded', () => {
  mount('dog', false)
  fireEvent.change(screen.getByLabelText('Camera'), { target: { value: 'rear' } })
  expect(screen.getByRole('status')).toHaveTextContent(
    'This camera was not recorded in this frame.',
  )
  expect(call).not.toHaveBeenCalled()
})
it('localizes camera names in Spanish', async () => {
  await i18n.changeLanguage('es')
  mount('dog')
  expect(screen.getByLabelText('Cámara')).toHaveTextContent('CabezaTrasera')
})
