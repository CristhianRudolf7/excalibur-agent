import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Scene } from './types'
const mocks = vi.hoisted(() => ({ renderer: vi.fn(), disconnect: vi.fn(), stopEvents: vi.fn() }))
vi.mock('three', async (original) => ({
  ...(await original<typeof import('three')>()),
  WebGLRenderer: class {
    constructor() {
      return mocks.renderer()
    }
  },
}))
vi.mock('three/addons/controls/OrbitControls.js', () => ({
  OrbitControls: class {
    target = { fromArray: vi.fn() }
    update = vi.fn()
    dispose = vi.fn()
  },
}))
vi.mock('./api', () => ({ onRobotEvent: () => mocks.stopEvents }))
vi.mock('./i18n', () => ({ useLabText: () => ({ text: (key: string) => key }) }))
import { Viewer } from './Viewer'
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
  vi.unstubAllGlobals()
})
const scene: Scene = {
  scenario: 'arm',
  params: {},
  timestep: 0.02,
  bodies: [],
  geoms: [],
  cameras: [],
  actuators: [],
  camera_default: { pos: [1, 1, 1], target: [0, 0, 0] },
}
function renderer() {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect = mocks.disconnect
    },
  )
  const result = {
    domElement: document.createElement('canvas'),
    shadowMap: {},
    setPixelRatio: vi.fn(),
    setAnimationLoop: vi.fn(),
    dispose: vi.fn(),
    forceContextLoss: vi.fn(),
  }
  mocks.renderer.mockReturnValue(result)
  return result
}
it('disposes the mounted canvas and scene resources on replacement and unmount', () => {
  const first = renderer()
  const view = render(<Viewer session="s" scene={scene} />)
  expect(screen.getByRole('img')).toContainElement(first.domElement)
  const second = renderer()
  view.rerender(<Viewer session="s" scene={{ ...scene }} />)
  expect(first.dispose).toHaveBeenCalledOnce()
  expect(first.forceContextLoss).toHaveBeenCalledOnce()
  expect(first.setAnimationLoop).toHaveBeenLastCalledWith(null)
  expect(first.domElement.isConnected).toBe(false)
  expect(screen.getByRole('img')).toContainElement(second.domElement)
  view.unmount()
  expect(second.dispose).toHaveBeenCalledOnce()
  expect(mocks.disconnect).toHaveBeenCalledTimes(2)
  expect(mocks.stopEvents).toHaveBeenCalledOnce()
})
it('shows WebGL failure and recovers when a new scene mounts successfully', () => {
  mocks.renderer.mockImplementationOnce(() => {
    throw new Error('No WebGL')
  })
  const view = render(<Viewer session="s" scene={scene} />)
  expect(screen.getByRole('status')).toHaveTextContent('WebGL')
  const next = renderer()
  view.rerender(<Viewer session="s" scene={{ ...scene }} />)
  expect(screen.queryByRole('status')).not.toBeInTheDocument()
  expect(screen.getByRole('img')).toContainElement(next.domElement)
})
