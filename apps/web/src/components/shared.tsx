import type { LucideIcon } from 'lucide-react'
import { useEffect, useState, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { PixelPortrait } from './PixelSprite'
import type { SpriteVariant } from './pixel-sprites'
import { Button } from './ui/button'
import { getAgent, type AgentId } from '@/features/shell/mock-data'
export function IconButton({
  label,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; children: ReactNode }) {
  const { t } = useTranslation()
  return (
    <Button aria-label={t(label)} title={t(label)} className="icon-button" {...props}>
      {children}
    </Button>
  )
}
export function PixelAvatar({ id, size = 28 }: { id: AgentId; size?: number }) {
  const agent = getAgent(id)
  const variants: Record<string, SpriteVariant> = {}
  return (
    <PixelPortrait
      sprite={{ variant: variants[id] ?? 'hood', tone: `var(--${agent.color})` }}
      size={size}
    />
  )
}

export type Status = 'pending' | 'progress' | 'review' | 'done' | 'blocked' | 'cancelled'
export function StatusIcon({ status, size = 18 }: { status: Status; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 20 20"
      className={`status-icon status-${status}`}
      aria-hidden="true"
    >
      <circle
        cx="10"
        cy="10"
        r="8.5"
        fill={status === 'done' ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.5"
      />
      {status === 'progress' && <path d="M10 1.5a8.5 8.5 0 0 0 0 17Z" fill="currentColor" />}
      {status === 'review' && <path d="M10 1.5a8.5 8.5 0 1 0 8.5 8.5H10Z" fill="currentColor" />}
      {status === 'done' && (
        <path d="m6 10 3 3 5-6" fill="none" stroke="var(--primary-foreground)" strokeWidth="1.7" />
      )}
      {status === 'blocked' && <path d="M5 10h10" stroke="currentColor" strokeWidth="2" />}
      {status === 'cancelled' && (
        <path d="m6 6 8 8m0-8-8 8" stroke="currentColor" strokeWidth="1.5" />
      )}
    </svg>
  )
}
export function Label({ label, color = 'primary' }: { label: string; color?: string }) {
  const { t } = useTranslation()
  return (
    <span className={`label tone-${color}`}>
      <i />
      {t(label)}
    </span>
  )
}

/** Translated page heading with an optional action group. */
export function PageHeader({
  eyebrow,
  title,
  subtitle,
  actions,
}: {
  eyebrow?: string
  title: string
  subtitle?: string
  actions?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <header className="page-header">
      <div>
        {eyebrow && <p className="page-eyebrow">{t(eyebrow)}</p>}
        <h1>{t(title)}</h1>
        {subtitle && <p className="page-subtitle">{t(subtitle)}</p>}
      </div>
      {actions && <div className="page-header-actions">{actions}</div>}
    </header>
  )
}
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon: LucideIcon
  title: string
  description: string
  action?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div className="empty-state">
      <span className="empty-state-icon">
        <Icon aria-hidden="true" />
      </span>
      <h2>{t(title)}</h2>
      <p>{t(description)}</p>
      {action}
    </div>
  )
}
export function Skeleton({ className }: { className?: string }) {
  return <span className={`skeleton ${className ?? ''}`} aria-hidden="true" />
}

/** Delay short requests and keep a revealed placeholder visible for 300ms. */
export function useLoading(pending: boolean) {
  const [visible, setVisible] = useState(false)
  const [shownAt, setShownAt] = useState(0)
  useEffect(() => {
    const timer = window.setTimeout(
      () => {
        setVisible(pending)
        if (pending && !visible) setShownAt(Date.now())
      },
      pending ? 150 : Math.max(0, 300 - (Date.now() - shownAt)),
    )
    return () => window.clearTimeout(timer)
  }, [pending, shownAt, visible])
  return { visible, busy: pending || visible }
}
export function LoadingStatus({
  label,
  children,
  className = '',
}: {
  label: string
  children: ReactNode
  className?: string
}) {
  return (
    <div
      className={`loading-layout ${className}`}
      role="status"
      aria-busy="true"
      aria-label={label}
    >
      {children}
    </div>
  )
}
