import type { FileChange } from '@/lib/rpc/contract'
import type { Message } from '@/features/chat/types'

/** Reverse a complete patch to recover the earliest known base. */
function baseContent(change: FileChange): string | undefined {
  if (change.binary || change.truncated || change.content === undefined) return undefined
  if (change.status === 'added') return ''
  const lines = change.content.split('\n')
  for (const hunk of [...change.hunks].reverse()) {
    if (hunk.lines.length > 10_000) return undefined
    const start = Math.max(0, hunk.new_start - 1)
    const expected = hunk.lines.filter((line) => !line.startsWith('-')).map((line) => line.slice(1))
    if (expected.some((line, index) => lines[start + index] !== line)) return undefined
    lines.splice(
      start,
      expected.length,
      ...hunk.lines.filter((line) => !line.startsWith('+')).map((line) => line.slice(1)),
    )
  }
  return lines.join('\n')
}
function combine(first: FileChange, latest: FileChange): FileChange {
  const base = baseContent(first)
  if (base === undefined || latest.content === undefined || latest.binary || latest.truncated)
    return latest
  const old = base === '' ? [] : base.replace(/\n$/, '').split('\n')
  const next = latest.content === '' ? [] : latest.content.replace(/\n$/, '').split('\n')
  let start = 0
  while (start < old.length && start < next.length && old[start] === next[start]) start++
  let endOld = old.length,
    endNew = next.length
  while (endOld > start && endNew > start && old[endOld - 1] === next[endNew - 1]) {
    endOld--
    endNew--
  }
  const before = old.slice(start, endOld),
    after = next.slice(start, endNew)
  const lines: string[] = []
  let added = 0,
    removed = 0
  if (!before.length || !after.length) {
    for (const line of before) lines.push(`-${line}`)
    for (const line of after) lines.push(`+${line}`)
    removed = before.length
    added = after.length
  } else {
    // Keep comparison work bounded for large files; the latest patch remains
    // authoritative when reconstructing a conversation-wide diff is too costly.
    const width = after.length + 1
    if ((before.length + 1) * width > 250_000) return latest
    const lengths = new Uint32Array((before.length + 1) * width)
    for (let i = before.length - 1; i >= 0; i--) {
      for (let j = after.length - 1; j >= 0; j--) {
        lengths[i * width + j] =
          before[i] === after[j]
            ? 1 + lengths[(i + 1) * width + j + 1]!
            : Math.max(lengths[(i + 1) * width + j]!, lengths[i * width + j + 1]!)
      }
    }
    let i = 0,
      j = 0
    while (i < before.length || j < after.length) {
      if (i < before.length && j < after.length && before[i] === after[j]) {
        lines.push(` ${before[i++]}`)
        j++
      } else if (
        i < before.length &&
        (j === after.length || lengths[(i + 1) * width + j]! >= lengths[i * width + j + 1]!)
      ) {
        lines.push(`-${before[i++]}`)
        removed++
      } else {
        lines.push(`+${after[j++]}`)
        added++
      }
    }
  }

  return {
    ...latest,
    status:
      latest.status === 'deleted' ? 'deleted' : first.status === 'added' ? 'added' : latest.status,
    added,
    removed,
    hunks:
      added || removed
        ? [
            {
              old_start: start + 1,
              old_lines: before.length,
              new_start: start + 1,
              new_lines: after.length,
              lines,
            },
          ]
        : [],
  }
}
export function conversationChanges(messages: Message[]): FileChange[] {
  const first = new Map<string, FileChange>(),
    latest = new Map<string, FileChange>()
  for (const message of messages) {
    if (message.sender !== 'agent') continue
    for (const change of message.changes ?? []) {
      first.set(change.path, first.get(change.path) ?? change)
      latest.set(change.path, change)
    }
  }
  return [...latest.values()].map((change) =>
    first.get(change.path) === change ? change : combine(first.get(change.path)!, change),
  )
}
