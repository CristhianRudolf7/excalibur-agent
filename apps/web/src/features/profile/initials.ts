export function initials(name: string) {
  const words = name.trim().split(/\s+/u).filter(Boolean)
  return words.length
    ? [words[0], ...(words.length > 1 ? [words.at(-1)] : [])]
        .map((word) => [...word!][0])
        .join('')
        .toLocaleUpperCase()
    : ''
}
