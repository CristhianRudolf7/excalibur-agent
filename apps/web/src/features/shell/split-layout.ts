export const splitRatioKey = 'chat-split-ratio-v2'
export const legacySplitRatioKey = 'panel-ratio'
export const minimumChatWidth = 360
export const minimumPanelWidth = 320
export const separatorWidth = 1

export function validSplitRatio(value: number) {
  return Number.isFinite(value) && value > 0 && value < 1
}

export function normalizeSplitRatio(value: number) {
  return validSplitRatio(value) ? value : 0.5
}

/** Migrate the legacy panel share to a chat share; pixel values are invalid. */
export function readSplitRatio() {
  try {
    const current = localStorage.getItem(splitRatioKey)
    const legacy = localStorage.getItem(legacySplitRatioKey)
    const ratio =
      current !== null
        ? normalizeSplitRatio(Number(current))
        : legacy !== null && validSplitRatio(Number(legacy))
          ? normalizeSplitRatio(1 - Number(legacy))
          : 0.5
    localStorage.setItem(splitRatioKey, String(ratio))
    localStorage.removeItem(legacySplitRatioKey)
    return ratio
  } catch {
    return 0.5
  }
}

/** Narrow containers use an overlay panel instead of violating either minimum. */
export function splitWidths(total: number, ratio: number) {
  const width = Number.isFinite(total) ? Math.max(0, total) : 0
  const overlay = width < minimumChatWidth + minimumPanelWidth + separatorWidth
  const available = overlay ? width : width - separatorWidth
  const chat = overlay
    ? width
    : Math.max(
        minimumChatWidth,
        Math.min(available * normalizeSplitRatio(ratio), available - minimumPanelWidth),
      )
  return { chat, panel: overlay ? width : available - chat, available, overlay }
}

/** Dragging operates on the panel edge, while persistence always stores the chat share. */
export function splitRatioFromPanel(panel: number, total: number): number | null {
  if (!Number.isFinite(panel) || !Number.isFinite(total) || splitWidths(total, 0.5).overlay)
    return null
  const available = total - separatorWidth
  const width = Math.max(minimumPanelWidth, Math.min(panel, available - minimumChatWidth))
  return (available - width) / available
}
