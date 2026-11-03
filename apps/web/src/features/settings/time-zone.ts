/** UTC remains available even when a browser's IANA list omits it. */
const fallbackZones = [
  'UTC',
  'America/Lima',
  'America/Bogota',
  'America/Mexico_City',
  'America/New_York',
  'America/Los_Angeles',
  'America/Sao_Paulo',
  'America/Argentina/Buenos_Aires',
  'Europe/London',
  'Europe/Madrid',
  'Europe/Paris',
  'Europe/Berlin',
  'Africa/Cairo',
  'Africa/Johannesburg',
  'Asia/Dubai',
  'Asia/Kolkata',
  'Asia/Kathmandu',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Australia/Sydney',
  'Pacific/Auckland',
]

export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

export function timeZones(current: string): string[] {
  let zones: string[] = fallbackZones
  try {
    zones = Intl.supportedValuesOf('timeZone')
  } catch {
    // Older browsers retain a useful list and the selected/browser zones.
  }
  return [...new Set(['UTC', current, browserTimeZone(), ...zones])].sort()
}

export function localClock(zone: string, language: string, now = new Date()): string {
  return new Intl.DateTimeFormat(language, {
    timeZone: zone,
    weekday: 'short',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'longOffset',
  }).format(now)
}
