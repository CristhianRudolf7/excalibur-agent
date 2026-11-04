export interface PublicPreview {
  url: string
  expiresAt: string
}

/** Standard Markdown keeps previews readable in history and other clients. Only
 * the daemon's explicit expiry title opts a link into the compact chat card. */
export function extractPublicPreviews(text: string) {
  const previews: PublicPreview[] = []
  const seen = new Set<string>()
  const body = text.replace(
    /\[Public preview\]\((https:\/\/[a-z0-9]+(?:-[a-z0-9]+)*\.trycloudflare\.com) "public-preview:([^"]+)"\)/g,
    (match, url: string, expiresAt: string) => {
      if (
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(expiresAt) ||
        !Number.isFinite(Date.parse(expiresAt))
      )
        return match
      if (!seen.has(url)) previews.push({ url, expiresAt })
      seen.add(url)
      return ''
    },
  )
  return { body: previews.length ? body.replace(/\n+$/, '') : text, previews }
}
