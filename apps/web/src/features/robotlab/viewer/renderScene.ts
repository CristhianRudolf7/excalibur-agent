import * as THREE from 'three'
import { OrbitControls } from 'three/addons/controls/OrbitControls.js'
import { applyPose, geomMesh, primitiveGeometry } from './geometry'
import { loadMesh } from './meshes'
import { isWall, observerOffset, wallOccludes } from './observer'
import type { Frame, Scene } from './types'

interface Options {
  scene: Scene
  view: 'observer' | 'camera'
  camera?: string
  frame: () => Frame | null
  ready: () => void
  meshError: () => void
  recenter: () => number
}

/** A mounted viewer owns all GPU resources; only decoded mesh data is shared. */
export function mountScene(node: HTMLDivElement, options: Options) {
  const { scene, view } = options
  const mobile = window.matchMedia?.('(pointer: coarse), (max-width: 767px)').matches ?? false
  const renderer = new THREE.WebGLRenderer({ antialias: !mobile, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, mobile ? 1.5 : 2))
  renderer.shadowMap.enabled = !mobile
  renderer.shadowMap.type = THREE.PCFSoftShadowMap
  renderer.domElement.style.display = 'block'
  renderer.domElement.style.width = '100%'
  renderer.domElement.style.height = '100%'
  node.appendChild(renderer.domElement)
  const world = new THREE.Scene()
  const bodies = scene.bodies.map((entry) => {
    const body = new THREE.Group()
    body.name = entry.name
    world.add(body)
    return body
  })
  const bounds = new THREE.Box3(
    new THREE.Vector3(...scene.bounds.min),
    new THREE.Vector3(...scene.bounds.max),
  )
  const center = bounds.getCenter(new THREE.Vector3())
  const radius = Math.max(bounds.getSize(new THREE.Vector3()).length() / 2, 0.1)
  let disposed = false
  const meshController = new AbortController()
  const walls: {
    mesh: THREE.Mesh<THREE.BufferGeometry, THREE.MeshStandardMaterial>
    alpha: number
  }[] = []
  for (const [index, body] of scene.bodies.entries()) {
    for (const geom of body.geoms) {
      if ((geom.rgba?.[3] ?? 1) <= 0) continue
      const geometry = primitiveGeometry(geom, radius * 4)
      if (geometry) {
        const mesh = geomMesh(geom, geometry, !mobile)
        bodies[index]!.add(mesh)
        if (view === 'observer' && isWall(geom, index)) {
          walls.push({ mesh, alpha: mesh.material.opacity })
        }
      } else if (geom.mesh) {
        void loadMesh(geom.mesh, meshController.signal).then(
          (loaded) => {
            if (disposed) loaded.dispose()
            else bodies[index]!.add(geomMesh(geom, loaded, !mobile))
          },
          () => {
            if (!disposed) options.meshError()
          },
        )
      }
    }
  }
  const theme = getComputedStyle(node)
  const sky = new THREE.Color(theme.getPropertyValue('--background').trim() || 'white')
  const ground = new THREE.Color(theme.getPropertyValue('--muted-foreground').trim() || 'gray')
  world.add(new THREE.HemisphereLight(sky, ground, 2.2))
  const light = new THREE.DirectionalLight(new THREE.Color(1, 1, 1), 2.5)
  light.position.copy(center).add(new THREE.Vector3(radius, -radius, radius * 2))
  light.target.position.copy(center)
  light.castShadow = !mobile
  light.shadow.mapSize.set(1024, 1024)
  light.shadow.camera.left = -radius
  light.shadow.camera.right = radius
  light.shadow.camera.top = radius
  light.shadow.camera.bottom = -radius
  light.shadow.camera.far = radius * 8
  light.shadow.normalBias = 0.015
  light.shadow.radius = 4
  world.add(light, light.target)
  const camera = new THREE.PerspectiveCamera(
    45,
    1,
    Math.max(radius / 1000, 0.001),
    Math.max(radius * 100, 100),
  )
  camera.up.set(0, 0, 1)
  let userControlled = false
  const controls = view === 'observer' ? new OrbitControls(camera, renderer.domElement) : null
  if (controls) {
    controls.target.copy(center)
    controls.enableDamping = true
    controls.screenSpacePanning = false
    controls.minDistance = radius * 0.02
    controls.maxDistance = radius * 30
    controls.addEventListener('start', () => {
      userControlled = true
    })
  }
  const direction = new THREE.Vector3(1, -1.3, 0.9).normalize()
  const frameObserver = () => {
    const vertical = THREE.MathUtils.degToRad(camera.fov)
    const horizontal = 2 * Math.atan(Math.tan(vertical / 2) * camera.aspect)
    const distance = (radius / Math.sin(Math.min(vertical, horizontal) / 2)) * 1.1
    camera.position.copy(controls?.target ?? center).addScaledVector(direction, distance)
    camera.lookAt(controls?.target ?? center)
    controls?.update()
  }
  if (view === 'observer') frameObserver()
  const baseIndex = scene.bodies.findIndex((body) => body.name === 'robot/base')
  const followIndex =
    baseIndex >= 0 ? baseIndex : scene.bodies.findIndex((body) => body.name.startsWith('robot/'))
  let framedRobot = false
  let recenter = options.recenter()
  let width = node.clientWidth
  let height = node.clientHeight
  let intersecting = true
  let animation: number | undefined
  let previous = performance.now()
  let ready = false
  const draw = (now: number) => {
    animation = undefined
    if (disposed || document.hidden || !intersecting || width <= 0 || height <= 0) return
    const elapsed = Math.min(Math.max((now - previous) / 1000, 0), 0.1)
    previous = now
    const frame = options.frame()
    if (frame) bodies.forEach((body, index) => applyPose(body, frame.poses, index * 7))
    if (view === 'observer' && controls) {
      if (recenter !== options.recenter()) {
        recenter = options.recenter()
        userControlled = false
        framedRobot = false
        if (!frame || followIndex < 0) frameObserver()
      }
      const base = frame && bodies[followIndex]
      if (base && !userControlled) {
        const blend = framedRobot ? 1 - Math.exp(-4 * elapsed) : 1
        controls.target.lerp(base.position, blend)
        const desired = controls.target.clone().add(observerOffset(scene.robot, base.quaternion))
        camera.position.lerp(desired, blend)
        camera.lookAt(controls.target)
        framedRobot = true
      }
      controls.update()
    } else {
      const name = options.camera ?? frame?.camera.name
      if (!frame || name !== frame.camera.name) {
        schedule()
        return
      }
      applyPose(camera, frame.camera.pose)
      const fovy = scene.cameras.find((entry) => entry.name === name)?.fovy ?? 58
      if (camera.fov !== fovy) {
        camera.fov = fovy
        camera.updateProjectionMatrix()
      }
    }
    const base = frame && bodies[followIndex]
    for (const { mesh, alpha } of walls) {
      const faded = !!base && wallOccludes(mesh, camera.position, base.position)
      mesh.material.opacity = faded ? Math.min(alpha, 0.15) : alpha
      mesh.material.transparent = faded || alpha < 1
      mesh.material.depthWrite = !faded && alpha >= 1
      mesh.castShadow = !mobile && !faded
    }
    renderer.render(world, camera)
    if (!ready) {
      ready = true
      options.ready()
    }
    schedule()
  }
  const schedule = () => {
    if (disposed || document.hidden || !intersecting || width <= 0 || height <= 0) {
      if (animation !== undefined) cancelAnimationFrame(animation)
      animation = undefined
    } else if (animation === undefined) {
      animation = requestAnimationFrame(draw)
    }
  }
  const resize = () => {
    width = node.clientWidth
    height = node.clientHeight
    if (width > 0 && height > 0) {
      renderer.setSize(width, height, false)
      camera.aspect = width / height
      camera.updateProjectionMatrix()
      if (view === 'observer' && !userControlled && !framedRobot) frameObserver()
    }
    schedule()
  }
  const sizeObserver = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(resize) : null
  sizeObserver?.observe(node)
  window.addEventListener('resize', resize)
  const visibility = () => schedule()
  document.addEventListener('visibilitychange', visibility)
  const intersection =
    typeof IntersectionObserver !== 'undefined'
      ? new IntersectionObserver((entries) => {
          intersecting = entries.some((entry) => entry.isIntersecting)
          schedule()
        })
      : null
  intersection?.observe(node)
  resize()
  return () => {
    disposed = true
    meshController.abort()
    if (animation !== undefined) cancelAnimationFrame(animation)
    sizeObserver?.disconnect()
    intersection?.disconnect()
    window.removeEventListener('resize', resize)
    document.removeEventListener('visibilitychange', visibility)
    controls?.dispose()
    world.traverse((object) => {
      if (object instanceof THREE.Mesh) {
        object.geometry.dispose()
        const materials = Array.isArray(object.material) ? object.material : [object.material]
        materials.forEach((material) => material.dispose())
      }
    })
    light.shadow.dispose()
    renderer.dispose()
    renderer.forceContextLoss()
    renderer.domElement.remove()
  }
}
