import type { Room, WorldLayout } from '../layout'
const room = (
  id: string,
  label: string,
  type: string,
  mainAgentId: string,
  x: number,
  y: number,
  footprint?: number[][],
): Room => ({
  id,
  label,
  type,
  mainAgentId,
  polygon: footprint
    ? footprint.map(([x, y]) => ({ x: x! * 1.5625, y: y! * 1.5625 }))
    : [
        { x: x - 135, y: y - 55 },
        { x, y: y - 135 },
        { x: x + 135, y: y - 55 },
        { x, y: y + 70 },
      ],
  spots: {
    desk: { x, y },
    reading: { x: x - 50, y: y + 20 },
    terminal: { x: x + 25, y },
    delivery: { x, y: y + 50 },
    rest: { x: x + 60, y: y + 100 },
  },
  door: { x, y: y + 90 },
  waypoints: [
    { x: 1180, y: 960 },
    { x: x < 1000 ? 850 : 1560, y: y + 160 },
    { x, y: y + 90 },
  ],
  background: footprint ? `/world/rooms/${id}.webp` : undefined,
  interiorSize: { width: 1600, height: 1000 },
  interiorSpots: {
    desk: { x: 800, y: 650 },
    reading: { x: 580, y: 680 },
    terminal: { x: 1010, y: 620 },
    delivery: { x: 800, y: 720 },
    rest: { x: 490, y: 740 },
    door: { x: 1250, y: 780 },
  },
  decorate: !footprint,
  deliverables: [
    {
      id: `${id}-result`,
      title: id === 'voice' ? 'worldDossier' : 'worldDeliverable',
      description: 'worldPreviewText',
      agentId: mainAgentId,
      panel: type === 'finance' ? 'sheet' : 'files',
      fileId:
        type === 'code' ? 'src/tests/auth.test.ts' : `results/${footprint ? id : 'library'}.md`,
      spot: { x: 800, y: 720 },
      ready: true,
    },
  ],
})
export const campus: WorldLayout = {
  id: 'garden-campus',
  width: 2400,
  height: 1600,
  background: '/world/campus.webp',
  fallbackBackground: '/world/rooms/library.webp',
  plaza: { x: 1180, y: 1130 },
  rooms: [
    room('library', 'worldLibrary', 'library', 'excalibur', 540, 355, [
      [95, 135],
      [335, 36],
      [607, 157],
      [351, 304],
    ]),
    room('code', 'worldCode', 'code', 'excalibur', 1910, 365, [
      [945, 138],
      [1189, 35],
      [1455, 155],
      [1212, 306],
    ]),
    room('voice', 'worldVoice', 'voice', 'excalibur', 1180, 770, [
      [587, 371],
      [770, 290],
      [969, 383],
      [772, 561],
    ]),
    room('design', 'design', 'design', 'excalibur', 430, 1010, [
      [67, 552],
      [274, 451],
      [486, 564],
      [280, 758],
    ]),
    room('finance', 'finance', 'finance', 'excalibur', 1900, 1310, [
      [1005, 775],
      [1216, 673],
      [1436, 785],
      [1236, 946],
    ]),
    room('mail', 'emailRole', 'mail', 'excalibur', 1990, 795, [
      [1119, 420],
      [1297, 348],
      [1484, 451],
      [1270, 590],
    ]),
    { ...room('launch', 'worldLaunch', 'project', 'excalibur', 840, 1380), projectId: 'launch' },
    {
      ...room('observatory', 'worldObservatory', 'garden', 'excalibur', 1370, 1430),
      projectId: 'observatory',
    },
  ],
}
