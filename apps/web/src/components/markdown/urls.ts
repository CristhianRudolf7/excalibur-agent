/** No relative, executable, data or application-specific protocols are admitted. */
export function safeMarkdownUrl(value: string, key = 'href') {
  const trimmed = value.trim()
  if (!/^(https?:|mailto:)/i.test(trimmed)) return ''
  try {
    const url = new URL(trimmed)
    return ['http:', 'https:', ...(key === 'href' ? ['mailto:'] : [])].includes(url.protocol)
      ? url.href
      : ''
  } catch {
    return ''
  }
}
