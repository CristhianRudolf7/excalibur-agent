import { useLabText, type LabMessage } from './i18n'
/** Small code-native illustrations keep the six scenario cards usable before installation. */
export function Thumbnail({ id, color = 'var(--primary)' }: { id: string; color?: string }) {
  const { text } = useLabText()
  const drawings: Record<string, string> = {
    cartpole: 'M25 68H95 M43 65V52H73V65Z M58 52L72 20 M69 20A3 3 0 1 0 75 20A3 3 0 1 0 69 20',
    ballplate: 'M22 58L62 40L102 58L62 75Z M62 74V84 M68 37A8 8 0 1 0 84 37A8 8 0 1 0 68 37',
    arm: 'M30 78H70 M50 77V60L30 40L58 20L83 42 M77 43L84 51L93 43 M24 39A6 6 0 1 0 36 39A6 6 0 1 0 24 39',
    quadruped: 'M33 45L84 42L91 58L39 61Z M40 60L29 80 M50 60L55 81 M77 58L72 80 M88 56L99 76',
    drone:
      'M60 40V65 M40 35L80 70 M80 35L40 70 M27 34H49 M70 34H92 M27 71H49 M70 71H92 M51 45H69V61H51Z',
    rocket: 'M48 65V36L60 16L72 36V65Z M48 51L35 73L48 67 M72 51L85 73L72 67 M53 72L60 88L67 72',
  }
  return (
    <svg
      className="lab-thumbnail"
      viewBox="0 0 120 100"
      role="img"
      aria-label={text(id as LabMessage)}
      style={{ color }}
    >
      <rect x="0" y="0" width="120" height="100" rx="12" fill="currentColor" opacity="0.07" />
      <path
        d={drawings[id] ?? drawings.arm}
        fill="none"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
