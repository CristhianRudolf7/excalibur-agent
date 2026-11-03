import type { Pose } from './contracts'
const bodyEdges = [
  [11, 12],
  [11, 13],
  [13, 15],
  [12, 14],
  [14, 16],
  [11, 23],
  [12, 24],
  [23, 24],
  [23, 25],
  [25, 27],
  [24, 26],
  [26, 28],
]
const handEdges = [
  [0, 1],
  [1, 2],
  [2, 3],
  [3, 4],
  [0, 5],
  [5, 6],
  [6, 7],
  [7, 8],
  [5, 9],
  [9, 10],
  [10, 11],
  [11, 12],
  [9, 13],
  [13, 14],
  [14, 15],
  [15, 16],
  [13, 17],
  [0, 17],
  [17, 18],
  [18, 19],
  [19, 20],
]
export function drawOverlay(canvas: HTMLCanvasElement, pose: Pose | null) {
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  if (!pose) return
  const color =
    getComputedStyle(canvas).getPropertyValue('--primary-strong').trim() ||
    getComputedStyle(canvas).color
  ctx.strokeStyle = color
  ctx.fillStyle = color
  ctx.lineWidth = 3
  const draw = (points: number[][], edges: number[][]) => {
    for (const [a, b] of edges) {
      const p = points[a!],
        q = points[b!]
      if (!p || !q || (p[3] ?? 1) < 0.65 || (q[3] ?? 1) < 0.65) continue
      ctx.beginPath()
      ctx.moveTo(p[0]! * canvas.width, p[1]! * canvas.height)
      ctx.lineTo(q[0]! * canvas.width, q[1]! * canvas.height)
      ctx.stroke()
    }
    for (const p of points) {
      if ((p[3] ?? 1) < 0.65) continue
      ctx.beginPath()
      ctx.arc(p[0]! * canvas.width, p[1]! * canvas.height, 3, 0, Math.PI * 2)
      ctx.fill()
    }
  }
  draw(pose.landmarks, bodyEdges)
  for (const hand of pose.hands ?? []) draw(hand.landmarks, handEdges)
}
