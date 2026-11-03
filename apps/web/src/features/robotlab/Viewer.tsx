import { useCallback, useEffect, useRef, useState } from 'react'
import * as THREE from 'three'
import { OrbitControls } from 'three/addons/controls/OrbitControls.js'
import type { Frame, Scene } from './types'
import { onRobotEvent } from './api'
import { FrameBuffer, geomMesh, inspectionCamera, toYUp, worldRotation } from './scene'
import { useLabText } from './i18n'
export interface ViewerProps {
  scene: Scene
  session: string
  preview?: boolean
  frame?: Frame
}
export function Viewer({ scene, session, preview = false, frame }: ViewerProps) {
  const buffer = useRef(new FrameBuffer())
  const [error, setError] = useState(false)
  const { text } = useLabText()
  useEffect(() => {
    if (frame) buffer.current.push(frame, performance.now())
  }, [frame])
  useEffect(() => {
    buffer.current = new FrameBuffer()
    return onRobotEvent('robotlab.frame', (event) => {
      if (event.session === session && Boolean(event.preview) === preview)
        buffer.current.push(event, performance.now())
    })
  }, [session, preview])
  const attachViewer = useCallback(
    (node: HTMLDivElement | null) => {
      if (!node) return
      let renderer: THREE.WebGLRenderer
      try {
        renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true })
      } catch {
        setError(true)
        return
      }
      setError(false)
      renderer.setPixelRatio(Math.min(devicePixelRatio, 2))
      renderer.shadowMap.enabled = !preview
      renderer.shadowMap.type = THREE.PCFSoftShadowMap
      node.appendChild(renderer.domElement)
      const world = new THREE.Scene()
      const root = new THREE.Group()
      root.rotation.x = worldRotation
      world.add(root)
      const bodies = scene.bodies.map(() => {
        const body = new THREE.Group()
        root.add(body)
        return body
      })
      for (const geom of scene.geoms) (bodies[geom.body] ?? root).add(geomMesh(geom))
      const theme = getComputedStyle(node)
      const grid = new THREE.GridHelper(
        20,
        80,
        theme.getPropertyValue('--strong-border').trim(),
        theme.getPropertyValue('--border').trim(),
      )
      const gridMaterials = Array.isArray(grid.material) ? grid.material : [grid.material]
      gridMaterials.forEach((material) => {
        material.transparent = true
        material.opacity = 0.3
        material.depthWrite = false
      })
      grid.position.y = 0.002
      world.add(grid)
      world.add(
        new THREE.HemisphereLight(new THREE.Color(1, 1, 1), new THREE.Color(0.4, 0.45, 0.5), 2.3),
      )
      const light = new THREE.DirectionalLight(new THREE.Color(1, 1, 1), 3)
      light.position.set(3, 8, 4)
      light.castShadow = true
      light.shadow.mapSize.set(1024, 1024)
      light.shadow.camera.left = -8
      light.shadow.camera.right = 8
      light.shadow.camera.top = 8
      light.shadow.camera.bottom = -8
      light.shadow.normalBias = 0.015
      light.shadow.radius = 4
      world.add(light)
      const camera = new THREE.PerspectiveCamera(45, 1, 0.01, 500)
      const framing = inspectionCamera(scene)
      camera.position.fromArray(toYUp(framing.pos))
      const controls = new OrbitControls(camera, renderer.domElement)
      controls.target.fromArray(toYUp(framing.target))
      controls.enableDamping = true
      controls.update()
      const resize = new ResizeObserver(() => {
        const width = node.clientWidth,
          height = node.clientHeight
        if (!width || !height) return
        renderer.setSize(width, height)
        camera.aspect = width / height
        camera.updateProjectionMatrix()
      })
      resize.observe(node)
      renderer.setAnimationLoop((now) => {
        buffer.current.sample(now)?.forEach((pose, i) => {
          const body = bodies[i]
          if (body) {
            body.position.set(pose[0], pose[1], pose[2])
            body.quaternion.set(pose[4], pose[5], pose[6], pose[3])
          }
        })
        controls.update()
        renderer.render(world, camera)
      })
      return () => {
        renderer.setAnimationLoop(null)
        resize.disconnect()
        controls.dispose()
        world.traverse((object) => {
          if (object instanceof THREE.Mesh || object instanceof THREE.LineSegments) {
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
    },
    [scene, preview],
  )
  return (
    <div className="lab-viewer" ref={attachViewer} role="img" aria-label={`${scene.scenario} 3D`}>
      {error && <span role="status">{text('unavailable')} · WebGL</span>}
    </div>
  )
}
