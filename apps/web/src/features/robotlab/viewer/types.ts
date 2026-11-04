import type { RobotLabFrame, RobotLabScene, RpcMethods } from '@/lib/rpc/contract'

export type Vec3 = [number, number, number]
export type Quaternion = [number, number, number, number]
export type Pose = [number, number, number, number, number, number, number]
export type Scene = RobotLabScene
export type SceneBody = Scene['bodies'][number]
export type SceneGeom = SceneBody['geoms'][number]
export type Frame = RobotLabFrame
export type MeshBlob = RpcMethods['robotlab.mesh.get']['result']
