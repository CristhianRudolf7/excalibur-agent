import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { sessionImage } from './api'
import { useLabText } from './i18n'

export function ActivityImage({ session, path }: { session: string; path: string }) {
  const { text } = useLabText()
  const container = useRef<HTMLDivElement>(null)
  const [visible, setVisible] = useState(() => typeof IntersectionObserver === 'undefined')
  useEffect(() => {
    if (!container.current || typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setVisible(true)
          observer.disconnect()
        }
      },
      { rootMargin: '200px' },
    )
    observer.observe(container.current)
    return () => observer.disconnect()
  }, [])
  const image = useQuery({
    queryKey: ['robotlab', 'session-image', session, path],
    queryFn: ({ signal }) => sessionImage({ session, path }, { signal }),
    enabled: visible,
    staleTime: Infinity,
    gcTime: 30 * 60 * 1000,
    retry: false,
  })
  return (
    <div ref={container} className="min-h-24">
      {image.data && (
        <img src={`data:image/png;base64,${image.data.png}`} alt={text('look')} loading="lazy" />
      )}
      {image.error && <p role="alert">{image.error.message}</p>}
    </div>
  )
}
