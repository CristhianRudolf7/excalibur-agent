import type { Page } from '@playwright/test'
import { mockEvents } from './event-fixture'
import type { Session, EnvironmentSummary } from '../src/features/robotlab/types'

export const observationPng =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII='
export function labSession(id: string, status: Session['status']): Session {
  return {
    id,
    name: id === 'old-training' ? 'Old training' : 'Live training',
    robot: 'dog',
    environment: 'room',
    task: 'Fetch ball',
    goal: { type: 'object_held', object: 'ball' },
    status,
    created_at: '2026-01-02T10:00:00Z',
    ended_at: status === 'live' ? null : '2026-01-02T10:05:00Z',
    max_attempts: 20,
    attempt: 2,
    attempts: [1, 2].map((n) => ({
      n,
      status: n === 1 ? 'failed' : status === 'live' ? 'running' : 'stopped',
      reason: '',
      started_at: '2026-01-02T10:00:00Z',
      ended_at: n === 2 && status === 'live' ? null : '2026-01-02T10:05:00Z',
      sim_time: 10,
      frames: 201,
      skills_used: ['fetch'],
    })),
    outcome: null,
    conversation: 'cnv_web_excalibur',
    skill_events: [
      { t: '2026-01-02T10:00:01Z', attempt: 2, kind: 'improved', skill: 'fetch', version: 2 },
    ],
  }
}
export async function mockRobotLab(page: Page, installedInitially = true) {
  const bus = await mockEvents(page)
  const calls: { method: string; params: Record<string, unknown> }[] = []
  const environments: EnvironmentSummary[] = [
    {
      id: 'room',
      name: 'Room',
      kind: 'room',
      objects: 1,
      updated_at: '2026-01-01',
      source_type: 'text',
    },
  ]
  const sessions = new Map([['old-training', labSession('old-training', 'stopped')]])
  let installed = installedInitially
  let settings = { max_attempts: 20, sim_speed: 1 }
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    calls.push(request)
    if (request.method === 'conversation.send') {
      if (request.params.text.startsWith('Create a simulation environment')) {
        environments.push({
          id: 'new-room',
          name: 'New room',
          kind: 'room',
          objects: 1,
          updated_at: '2026-01-03',
          source_type: 'text',
        })
        bus.emit('robotlab.session', { session: sessions.get('old-training') })
      }
      if (/^(Run a simulation|Realiza una simulación)/.test(request.params.text)) {
        const session = labSession('live-training', 'live')
        sessions.set(session.id, session)
        bus.emit('robotlab.focus', {
          session: session.id,
          robot: session.robot,
          environment: session.environment,
        })
        bus.emit('robotlab.session', { session })
      }
      return route.fallback()
    }
    if (!request.method.startsWith('robotlab.')) return route.fallback()
    const params = request.params ?? {}
    let result: unknown = {}
    switch (request.method) {
      case 'robotlab.status':
        result = {
          installed,
          enabled: true,
          running: installed,
          python: 'python3',
          requirements: { python: 'python3', osmesa: true },
        }
        break
      case 'robotlab.install':
        installed = true
        result = { started: true }
        bus.emit('robotlab.install', { phase: 'done', done: true, percent: 100, message: 'Ready' })
        break
      case 'robotlab.env.list':
        result = { environments }
        break
      case 'robotlab.session.image':
      case 'robotlab.env.thumbnail':
        result = { png: observationPng }
        break
      case 'robotlab.env.get':
        result = {
          environment: {
            ...environments.find((env) => env.id === params.id),
            version: 1,
            objects: [
              {
                id: 'ball',
                name: 'Ball',
                shape: 'sphere',
                size: [0.2],
                pos: [0, 0, 0.1],
                color: '#ff6600',
                dynamic: true,
              },
            ],
          },
        }
        break
      case 'robotlab.env.delete': {
        const index = environments.findIndex((env) => env.id === params.id)
        if (index >= 0) environments.splice(index, 1)
        break
      }
      case 'robotlab.media.upload':
        result = { media_id: 'media-1', kind: 'image', frames: 1 }
        break
      case 'robotlab.robots.list':
        result = {
          robots: ['arm', 'dog', 'drone', 'humanoid'].map((id) => ({
            id,
            name: { en: id, es: id },
            description: '',
            sensors: [],
            cameras: [],
            tools: [],
            limits: {},
            notes: [],
          })),
        }
        break
      case 'robotlab.session.list':
        result = {
          sessions: [...sessions.values()].map((session) => ({
            ...session,
            attempts: session.attempts.length,
            live: session.status === 'live',
          })),
        }
        break
      case 'robotlab.session.get':
        result = { session: sessions.get(params.session) }
        break
      case 'robotlab.session.stop': {
        const session = sessions.get(params.session)!
        session.status = 'stopped'
        result = { session }
        bus.emit('robotlab.session', { session })
        break
      }
      case 'robotlab.session.scene':
      case 'robotlab.env.scene':
        result = {
          scene: {
            version: 1,
            bodies: [
              {
                name: 'world',
                geoms: [
                  {
                    type: 'box',
                    size: [2.5, 2, 0.05],
                    pos: [0, 0, -0.05],
                    quat: [1, 0, 0, 0],
                    rgba: [0.6, 0.6, 0.6, 1],
                  },
                ],
              },
              {
                name: 'obj/ball',
                geoms: [
                  {
                    type: 'sphere',
                    size: [0.1],
                    pos: [0, 0, 0],
                    quat: [1, 0, 0, 0],
                    rgba: [0.8, 0.4, 0.1, 1],
                  },
                ],
              },
            ],
            objects: [{ id: 'ball', name: 'Ball', body: 1, dynamic: true }],
            cameras: [{ name: 'robot/head', fovy: 58 }],
            bounds: { min: [-2.5, -2, 0], max: [2.5, 2, 2] },
            robot: request.method === 'robotlab.env.scene' ? null : 'dog',
            environment: 'room',
          },
        }
        break
      case 'robotlab.attempt.frames':
        result = {
          total: 201,
          frames: Array.from({ length: 201 }, (_, index) => ({
            session: params.session,
            attempt: params.attempt,
            seq: index,
            t: index / 20,
            poses: [0, 0, 0, 1, 0, 0, 0, index / 100, 0, 0.1, 1, 0, 0, 0],
            camera: { name: 'robot/head', pose: [0, -2, 2, 0.9239, 0.3827, 0, 0] },
            held: null,
            status: 'running',
          })),
        }
        break
      case 'robotlab.session.log':
        result = {
          entries: [
            {
              id: 'look-1',
              t: '2026-01-02T10:00:00Z',
              session: params.session,
              attempt: params.attempt,
              kind: 'look',
              summary: 'The ball is visible',
              image: `attempts/${params.attempt}/looks/1.png`,
              sim_time: 0,
            },
            {
              id: 'reflection-1',
              t: '2026-01-02T10:00:01Z',
              session: params.session,
              attempt: params.attempt,
              kind: 'reflection',
              summary: 'Approach slowly',
              sim_time: 0.5,
            },
          ],
          next: 2,
        }
        break
      case 'robotlab.skills.list':
        result = {
          skills: [
            {
              name: 'fetch',
              description: 'Fetch the ball',
              version: 2,
              success_rate: 0.5,
              uses: 2,
              has_code: true,
              failures: 1,
              updated_at: '2026-01-02',
            },
          ],
        }
        break
      case 'robotlab.settings.get':
        result = settings
        break
      case 'robotlab.settings.set':
        settings = { ...settings, ...params }
        result = settings
        break
    }
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  return { bus, calls, sessions }
}

/** Primitive-only humanoid in the reported 8 x 2 m corridor, with unnamed walls. */
export function walledObserverScene() {
  const box = (size: number[], pos: number[], rgba: number[]) => ({
    type: 'box' as const,
    size,
    pos,
    quat: [1, 0, 0, 0],
    rgba,
  })
  const body = [0.22, 0.36, 0.48, 1]
  const joint = [0.13, 0.16, 0.2, 1]
  const wall = [0.85, 0.82, 0.76, 1]
  return {
    version: 1,
    bodies: [
      {
        name: 'world',
        geoms: [
          box([4, 1, 0.05], [0, 0, -0.05], [0.6, 0.61, 0.58, 1]),
          box([4, 0.05, 1.25], [0, 1, 1.25], wall),
          box([4, 0.05, 1.25], [0, -1, 1.25], wall),
          box([0.05, 1, 1.25], [-4, 0, 1.25], wall),
          box([0.05, 1, 1.25], [4, 0, 1.25], wall),
        ],
      },
      {
        name: 'robot/base',
        geoms: [
          box([0.15, 0.2, 0.27], [0, 0, 0.15], body),
          box([0.13, 0.18, 0.1], [0, 0, -0.2], joint),
          box([0.1, 0.1, 0.12], [0, 0, 0.58], body),
          box([0.015, 0.08, 0.04], [0.105, 0, 0.6], joint),
          ...[-1, 1].flatMap((side) => [
            box([0.07, 0.07, 0.25], [0, side * 0.29, 0.12], body),
            box([0.065, 0.075, 0.27], [0, side * 0.12, -0.55], body),
            box([0.13, 0.085, 0.04], [0.06, side * 0.12, -0.8], joint),
          ]),
        ],
      },
    ],
    objects: [],
    cameras: [{ name: 'robot/head', fovy: 58 }],
    bounds: { min: [-4, -1, 0], max: [4, 1, 2.5] },
    robot: 'humanoid',
    environment: 'room',
  }
}
