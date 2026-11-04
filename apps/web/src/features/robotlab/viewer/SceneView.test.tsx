import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import * as THREE from 'three'
import i18n from '@/lib/i18n'
const mocks = vi.hoisted(() => ({
  renderer: vi.fn(),
  mesh: vi.fn(),
  resizeDisconnect: vi.fn(),
  intersectionDisconnect: vi.fn(),
  controls: [] as { target: import('three').Vector3; start: () => void; dispose: () => void }[],
}))
vi.mock('three', async (original) => ({
  ...(await original<typeof import('three')>()),
  WebGLRenderer: class {
    constructor() {
      return mocks.renderer()
    }
  },
}))
vi.mock('three/addons/controls/OrbitControls.js', async () => {
  const { Vector3 } = await import('three')
  return {
    OrbitControls: class {
      target = new Vector3()
      start = () => {}
      update = vi.fn()
      dispose = vi.fn()
      constructor() {
        mocks.controls.push(this)
      }
      addEventListener(_type: string, callback: () => void) {
        this.start = callback
      }
    },
  }
})
vi.mock('./meshes', () => ({ loadMesh: mocks.mesh }))
import { SceneView } from './SceneView'
import { frameFixture, sceneFixture } from './fixtures.test-support'
import type { Scene } from './types'

let resize: () => void
let intersect: (entries: { isIntersecting: boolean }[]) => void
let width = 640
let height = 400
let hidden = false
let now = 0
let next = 0
const callbacks = new Map<number, FrameRequestCallback>()
function renderer() {
  const result = {
    domElement: document.createElement('canvas'),
    shadowMap: { enabled: false, type: 0 },
    setPixelRatio: vi.fn(),
    setSize: vi.fn(),
    render: vi.fn<(world: THREE.Scene, camera: THREE.PerspectiveCamera) => void>(),
    dispose: vi.fn(),
    forceContextLoss: vi.fn(),
  }
  mocks.renderer.mockReturnValue(result)
  return result
}
beforeEach(() => {
  mocks.renderer.mockReset()
  mocks.mesh.mockReset()
  mocks.resizeDisconnect.mockReset()
  mocks.intersectionDisconnect.mockReset()
  mocks.controls.length = 0
  callbacks.clear()
  width = 640
  height = 400
  hidden = false
  now = 0
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => width)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(() => height)
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden)
  vi.spyOn(performance, 'now').mockImplementation(() => now)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    callbacks.set(++next, callback)
    return next
  })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => callbacks.delete(id))
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback: () => void) {
        resize = callback
      }
      observe() {}
      disconnect = mocks.resizeDisconnect
    },
  )
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(callback: typeof intersect) {
        intersect = callback
      }
      observe() {}
      disconnect = mocks.intersectionDisconnect
    },
  )
  vi.stubGlobal('matchMedia', () => ({ matches: false }))
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
function tick(ms = 16) {
  act(() => {
    now += ms
    const pending = [...callbacks.values()]
    callbacks.clear()
    pending.forEach((callback) => callback(now))
  })
}
const scene: Scene = {
  ...sceneFixture,
  bodies: [
    { name: 'world', geoms: [{ type: 'plane', size: [2, 2], rgba: [1, 1, 1, 1] }] },
    {
      name: 'robot/base',
      geoms: [
        {
          type: 'box',
          size: [0.1, 0.2, 0.3],
          pos: [0.1, 0, 0],
          quat: [1, 0, 0, 0],
          rgba: [0.5, 0.5, 0.5, 1],
        },
      ],
    },
  ],
}
it('builds per-body groups, applies frame poses, preserves renderer across frame updates and cleans up resources', () => {
  const graphics = renderer()
  const disposeGeometry = vi.spyOn(THREE.BufferGeometry.prototype, 'dispose')
  const disposeMaterial = vi.spyOn(THREE.Material.prototype, 'dispose')
  const ready = vi.fn()
  const view = render(
    <SceneView
      scene={scene}
      frame={frameFixture()}
      view="observer"
      label="Observer"
      onReady={ready}
    />,
  )
  tick()
  expect(screen.getByRole('img', { name: 'Observer' })).toContainElement(graphics.domElement)
  expect(ready).toHaveBeenCalledOnce()
  const [world, camera] = graphics.render.mock.calls[0]!
  expect(camera.up.toArray()).toEqual([0, 0, 1])
  const body = world.getObjectByName('robot/base')!
  expect(body.position.toArray()).toEqual([0, 0, 0.5])
  expect(body.children).toHaveLength(1)
  view.rerender(
    <SceneView
      scene={scene}
      frame={frameFixture(20)}
      view="observer"
      label="Observer"
      onReady={ready}
    />,
  )
  tick()
  expect(mocks.renderer).toHaveBeenCalledOnce()
  expect(body.position.toArray()).toEqual([1, 0, 0.5])
  expect(ready).toHaveBeenCalledOnce()
  view.unmount()
  expect(graphics.dispose).toHaveBeenCalledOnce()
  expect(graphics.forceContextLoss).toHaveBeenCalledOnce()
  expect(mocks.resizeDisconnect).toHaveBeenCalledOnce()
  expect(mocks.intersectionDisconnect).toHaveBeenCalledOnce()
  expect(mocks.controls[0]!.dispose).toHaveBeenCalledOnce()
  expect(disposeGeometry).toHaveBeenCalledTimes(2)
  expect(disposeMaterial).toHaveBeenCalledTimes(2)
  expect(callbacks.size).toBe(0)
  expect(graphics.domElement.isConnected).toBe(false)
})
it('renders the MuJoCo camera quaternion directly with its fovy and no orbit controls', () => {
  const graphics = renderer()
  const frame = frameFixture(0, { camera: { name: 'robot/head', pose: [1, 2, 3, 0, 0, 1, 0] } })
  const view = render(<SceneView scene={scene} frame={frame} view="camera" label="Camera" />)
  tick()
  const [, camera] = graphics.render.mock.calls[0]!
  expect(camera.position.toArray()).toEqual([1, 2, 3])
  expect(camera.quaternion.toArray()).toEqual([0, 1, 0, 0])
  expect(camera.fov).toBe(58)
  expect(
    camera.getWorldDirection(new THREE.Vector3()).distanceTo(new THREE.Vector3(0, 0, 1)),
  ).toBeCloseTo(0)
  expect(mocks.controls).toHaveLength(0)
  view.rerender(
    <SceneView scene={scene} frame={frame} view="camera" camera="robot/wrist" label="Camera" />,
  )
  tick()
  expect(screen.getByRole('status')).toBeInTheDocument()
  expect(graphics.render).toHaveBeenCalledOnce()
})
it('resizes the viewport and pauses when the document, viewport or element is hidden', () => {
  const graphics = renderer()
  render(<SceneView scene={scene} frame={null} view="observer" label="Environment" />)
  tick()
  expect(graphics.setSize).toHaveBeenLastCalledWith(640, 400, false)
  act(() => {
    width = 300
    height = 500
    resize()
  })
  tick()
  expect(graphics.render.mock.calls.at(-1)![1].aspect).toBe(0.6)
  act(() => {
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
  })
  expect(callbacks.size).toBe(0)
  const count = graphics.render.mock.calls.length
  tick()
  expect(graphics.render).toHaveBeenCalledTimes(count)
  act(() => {
    hidden = false
    document.dispatchEvent(new Event('visibilitychange'))
  })
  tick()
  expect(graphics.render).toHaveBeenCalledTimes(count + 1)
  act(() => intersect([{ isIntersecting: false }]))
  expect(callbacks.size).toBe(0)
  act(() => intersect([{ isIntersecting: true }]))
  expect(callbacks.size).toBe(1)
  act(() => {
    height = 0
    resize()
  })
  expect(callbacks.size).toBe(0)
})
it('follows the robot until user orbit and resumes framing on Recenter', () => {
  const graphics = renderer()
  const view = render(
    <SceneView scene={scene} frame={frameFixture()} view="observer" label="Observer" />,
  )
  tick()
  const controls = mocks.controls[0]!
  const start = controls.target.clone()
  expect(start.toArray()).toEqual([0, 0, 0.5])
  const camera = graphics.render.mock.calls[0]![1]
  expect(camera.position.distanceTo(start)).toBeCloseTo(3.5)
  view.rerender(
    <SceneView scene={scene} frame={frameFixture(20)} view="observer" label="Observer" />,
  )
  tick(50)
  expect(controls.target.x).toBeGreaterThan(start.x)
  expect(controls.target.x).toBeLessThan(start.x + 1)
  controls.start()
  const afterOrbit = controls.target.clone()
  view.rerender(
    <SceneView scene={scene} frame={frameFixture(40)} view="observer" label="Observer" />,
  )
  tick(50)
  expect(controls.target.toArray()).toEqual(afterOrbit.toArray())
  fireEvent.click(screen.getByRole('button', { name: /Recenter|Recentrar/ }))
  tick()
  expect(controls.target.toArray()).toEqual([2, 0, 0.5])
  expect(camera.position.distanceTo(controls.target)).toBeCloseTo(3.5)
})
it('disables shadows and limits pixel ratio on mobile', () => {
  vi.stubGlobal('matchMedia', () => ({ matches: true }))
  const graphics = renderer()
  render(<SceneView scene={scene} frame={null} view="observer" label="Environment" />)
  tick()
  expect(graphics.shadowMap.enabled).toBe(false)
  const world = graphics.render.mock.calls[0]![0]
  world.traverse((object) => {
    if (object instanceof THREE.Mesh) expect(object.castShadow).toBe(false)
  })
})
it('shows localized fallback text without WebGL, waits for null scenes and recovers on replacement', () => {
  mocks.renderer.mockImplementationOnce(() => {
    throw new Error('WebGL unavailable')
  })
  const view = render(<SceneView scene={scene} frame={null} view="observer" label="Environment" />)
  expect(screen.getByRole('status')).toHaveTextContent('WebGL')
  const graphics = renderer()
  view.rerender(<SceneView scene={{ ...scene }} frame={null} view="observer" label="Environment" />)
  tick()
  expect(screen.queryByRole('status')).not.toBeInTheDocument()
  expect(graphics.render).toHaveBeenCalledOnce()
  view.rerender(<SceneView scene={null} frame={null} view="observer" label="Environment" />)
  expect(screen.getByRole('status')).toBeInTheDocument()
  expect(graphics.dispose).toHaveBeenCalledOnce()
})
it('shows scene loading text during retries in both languages', async () => {
  await i18n.changeLanguage('es')
  const view = render(
    <SceneView scene={null} loading frame={null} view="observer" label="Observer" />,
  )
  expect(screen.getByRole('status')).toHaveTextContent('Cargando escena…')
  await act(() => i18n.changeLanguage('en'))
  expect(screen.getByRole('status')).toHaveTextContent('Loading scene…')
  view.unmount()
})
it('disposes meshes that finish loading after unmount and reports mesh failures', async () => {
  renderer()
  let finish: (geometry: THREE.BufferGeometry) => void = () => {}
  mocks.mesh.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const meshScene: Scene = {
    ...sceneFixture,
    bodies: [{ name: 'world', geoms: [{ type: 'mesh', mesh: 'late' }] }],
  }
  const view = render(
    <SceneView scene={meshScene} frame={null} view="observer" label="Environment" />,
  )
  const signal = mocks.mesh.mock.calls[0]![1] as AbortSignal
  view.unmount()
  expect(signal.aborted).toBe(true)
  const geometry = new THREE.BufferGeometry()
  const dispose = vi.spyOn(geometry, 'dispose')
  await act(async () => finish(geometry))
  expect(dispose).toHaveBeenCalledOnce()
  mocks.mesh.mockRejectedValueOnce(new Error('offline'))
  render(<SceneView scene={meshScene} frame={null} view="observer" label="Environment" />)
  expect(await screen.findByRole('status')).toBeInTheDocument()
})
it('renders loaded meshes in their body local frame', async () => {
  const graphics = renderer()
  mocks.mesh.mockResolvedValueOnce(new THREE.BoxGeometry(1, 1, 1))
  const meshScene: Scene = {
    ...sceneFixture,
    bodies: [
      {
        name: 'robot/base',
        geoms: [{ type: 'mesh', mesh: 'box', pos: [1, 2, 3], scale: [2, 2, 2] }],
      },
    ],
  }
  render(<SceneView scene={meshScene} frame={null} view="observer" label="Environment" />)
  await act(async () => {})
  tick()
  const world = graphics.render.mock.calls[0]![0]
  const mesh = world.getObjectByName('robot/base')!.children[0]!
  expect(mesh.position.toArray()).toEqual([1, 2, 3])
  expect(mesh.scale.toArray()).toEqual([2, 2, 2])
})

it('fades the near unnamed wall per frame and restores it after orbit, only in observer view', () => {
  const graphics = renderer()
  const walled: Scene = {
    ...scene,
    bodies: [
      {
        name: 'world',
        geoms: [
          { type: 'box', size: [4, 0.05, 1.25], pos: [0, 1, 1.25] },
          { type: 'box', size: [4, 0.05, 1.25], pos: [0, -1, 1.25] },
        ],
      },
      scene.bodies[1]!,
    ],
  }
  const view = render(
    <SceneView scene={walled} frame={frameFixture()} view="observer" label="Observer" />,
  )
  tick()
  const [world, camera] = graphics.render.mock.calls[0]!
  const [near, far] = world.getObjectByName('world')!.children as THREE.Mesh<
    THREE.BufferGeometry,
    THREE.MeshStandardMaterial
  >[]
  expect(near!.material.opacity).toBe(0.15)
  expect(near!.material.depthWrite).toBe(false)
  expect(near!.castShadow).toBe(false)
  expect(far!.material.opacity).toBe(1)
  mocks.controls[0]!.start()
  camera.position.set(-2, -2, 2)
  tick()
  expect(near!.material.opacity).toBe(1)
  expect(near!.material.depthWrite).toBe(true)
  expect(near!.castShadow).toBe(true)
  expect(far!.material.opacity).toBe(0.15)
  view.rerender(<SceneView scene={walled} frame={frameFixture()} view="camera" label="Camera" />)
  tick()
  const cameraWall = graphics.render.mock.calls.at(-1)![0].getObjectByName('world')!
    .children[0] as THREE.Mesh<THREE.BufferGeometry, THREE.MeshStandardMaterial>
  expect(cameraWall.material.opacity).toBe(1)
})
