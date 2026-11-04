import * as THREE from 'three'
import { TransportError } from '@/lib/rpc/client'
import { viewerCall } from './rpc'
import { transientViewerError, waitForViewerRetry } from './retry'
import type { MeshBlob } from './types'

interface MeshData {
  positions: Float32Array
  indices: Uint32Array
}
// Cache CPU data, not GPU resources: each mounted renderer owns and disposes its geometry.
interface MeshRequest {
  promise: Promise<MeshData>
  controller: AbortController
  users: number
  done: boolean
}
const meshCache = new Map<string, MeshRequest>()
function bytes(encoded: string) {
  return Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))
}
export function decodeMesh(blob: MeshBlob): MeshData {
  const positions = bytes(blob.positions)
  const indices = bytes(blob.indices)
  if (positions.length !== blob.vertices * 12 || indices.length !== blob.faces * 12)
    throw new Error('Invalid mesh dimensions')
  const positionView = new DataView(positions.buffer)
  const indexView = new DataView(indices.buffer)
  const result = {
    positions: new Float32Array(blob.vertices * 3),
    indices: new Uint32Array(blob.faces * 3),
  }
  for (let i = 0; i < result.positions.length; i++) {
    const value = positionView.getFloat32(i * 4, true)
    if (!Number.isFinite(value)) throw new Error('Invalid mesh position')
    result.positions[i] = value
  }
  for (let i = 0; i < result.indices.length; i++) {
    const index = indexView.getUint32(i * 4, true)
    if (index >= blob.vertices) throw new Error('Invalid mesh index')
    result.indices[i] = index
  }
  return result
}
async function fetchMesh(key: string, signal: AbortSignal): Promise<MeshData> {
  for (let attempt = 0; ; attempt++) {
    let blob: MeshBlob
    try {
      blob = await viewerCall('robotlab.mesh.get', { key }, { signal })
    } catch (error) {
      if (signal.aborted || !transientViewerError(error)) throw error
      await waitForViewerRetry(attempt, signal)
      continue
    }
    return decodeMesh(blob)
  }
}

export async function loadMesh(key: string, signal?: AbortSignal) {
  if (signal?.aborted) throw new TransportError('Request aborted', 'aborted')
  let request = meshCache.get(key)
  if (!request) {
    const controller = new AbortController()
    const entry: MeshRequest = {
      controller,
      users: 0,
      done: false,
      promise: fetchMesh(key, controller.signal).then(
        (data) => {
          entry.done = true
          return data
        },
        (error: unknown) => {
          entry.done = true
          if (meshCache.get(key) === entry) meshCache.delete(key)
          throw error
        },
      ),
    }
    meshCache.set(key, entry)
    request = entry
  }
  request.users++
  let data: MeshData
  try {
    data = await new Promise<MeshData>((resolve, reject) => {
      const abort = () => reject(new TransportError('Request aborted', 'aborted'))
      signal?.addEventListener('abort', abort, { once: true })
      void request.promise.then(
        (value) => {
          signal?.removeEventListener('abort', abort)
          resolve(value)
        },
        (error: unknown) => {
          signal?.removeEventListener('abort', abort)
          reject(error)
        },
      )
    })
  } finally {
    request.users--
    if (!request.users && !request.done) {
      if (meshCache.get(key) === request) meshCache.delete(key)
      request.controller.abort()
    }
  }
  const geometry = new THREE.BufferGeometry()
  geometry.setAttribute('position', new THREE.BufferAttribute(data.positions, 3))
  geometry.setIndex(new THREE.BufferAttribute(data.indices, 1))
  geometry.computeVertexNormals()
  return geometry
}
