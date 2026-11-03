import { AgentSettings } from '@/features/agents/AgentSettings'
import { useArtifacts } from '@/features/artifacts/store'
import { loadingLazy as lazy, LoadingFallback } from '@/features/shell/loading'
import { LoadingStatus, Skeleton, useLoading } from '@/components/shared'
import { UsageSkeleton } from './Skeletons'
import { useEffect, useRef, useState, Suspense } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  SlidersHorizontal,
  UserRound,
  ChartNoAxesCombined,
  Sparkles,
  LockKeyhole,
  Users,
} from 'lucide-react'
import { useProfile } from '@/features/profile/api'
import { TimeZone } from './TimeZone'
import { GoogleConnect } from './GoogleConnect'
import { Segmented } from './Segmented'
import { Skills } from './Skills'
import { Passwords } from './Passwords'
import { Feedback } from './feedback'
import './modern.css'
const Usage = lazy(() => import('./Usage'))
const sections = [
  { id: 'general', label: 'settingsGeneral', icon: SlidersHorizontal },
  { id: 'profile', label: 'profileTitle', icon: UserRound },
  { id: 'agents', label: 'agentSettings', icon: Users },
  { id: 'usage', label: 'settingsUsage', icon: ChartNoAxesCombined },
  { id: 'skills', label: 'skills', icon: Sparkles },
  { id: 'connections', label: 'googleConnections', icon: SlidersHorizontal },
  { id: 'passwords', label: 'passwordsTitle', icon: LockKeyhole },
] as const
function General() {
  const { t, i18n } = useTranslation()
  const autoOpen = useArtifacts((state) => state.autoOpen)
  const setAutoOpen = useArtifacts((state) => state.setAutoOpen)
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))
  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
    try {
      localStorage.setItem('excalibur-ui-theme', dark ? 'dark' : 'light')
    } catch {
      /* Optional preference. */
    }
  }, [dark])
  return (
    <div className="settings-card settings-general">
      <div className="settings-preference">
        <div className="settings-preference-copy">
          <h3>{t('settingsLanguage')}</h3>
          <p>{t('settingsLanguageNote')}</p>
        </div>
        <Segmented
          label={t('settingsLanguage')}
          value={i18n.language.startsWith('en') ? 'en' : 'es'}
          options={[
            { value: 'es', label: t('settingsSpanish') },
            { value: 'en', label: t('settingsEnglish') },
          ]}
          onChange={(value) => void i18n.changeLanguage(value)}
        />
      </div>
      <div className="settings-preference settings-preference-toggle">
        <div className="settings-preference-copy">
          <h3 id="settings-auto-open-label">{t('artifactAutoOpen')}</h3>
          <p id="settings-auto-open-note">{t('settingsAutoOpenNote')}</p>
        </div>
        <button
          type="button"
          role="switch"
          className="settings-switch"
          aria-labelledby="settings-auto-open-label"
          aria-describedby="settings-auto-open-note"
          aria-checked={autoOpen}
          onClick={() => setAutoOpen(!autoOpen)}
        >
          <span />
        </button>
      </div>
      <TimeZone />
      <div className="settings-preference">
        <div className="settings-preference-copy">
          <h3>{t('settingsTheme')}</h3>
          <p>{t('settingsThemeNote')}</p>
        </div>
        <Segmented
          label={t('settingsTheme')}
          value={dark ? 'dark' : 'light'}
          options={[
            { value: 'light', label: t('settingsLight') },
            { value: 'dark', label: t('settingsDark') },
          ]}
          onChange={(value) => setDark(value === 'dark')}
        />
      </div>
    </div>
  )
}
function Profile() {
  const { t } = useTranslation()
  const profile = useProfile()
  const loading = useLoading(profile.isPending)
  return (
    <div className="settings-card settings-profile">
      <Feedback
        loading={loading}
        query={profile}
        skeleton={
          <LoadingStatus label={t('settingsLoading')} className="settings-profile-skeleton">
            <Skeleton className="sk-title" />
            <Skeleton className="sk-field" />
          </LoadingStatus>
        }
      />
      {!loading.busy && profile.data && (
        <>
          <label htmlFor="principal-name">
            {t('profileName')} · {t('settingsReadOnly')}
          </label>
          <input id="principal-name" readOnly value={profile.data.name} />
        </>
      )}
    </div>
  )
}
export default function Settings() {
  const { t } = useTranslation()
  const search = useSearch({ strict: false }) as { section?: string }
  const navigate = useNavigate()
  const section = sections.find((s) => s.id === search.section) ?? sections[0]
  const navRef = useRef<HTMLElement>(null)
  useEffect(() => {
    const revealCurrent = () => {
      const nav = navRef.current
      const current = nav?.querySelector<HTMLButtonElement>('[aria-current="page"]')
      if (nav && current && nav.scrollWidth > nav.clientWidth) {
        nav.scrollLeft = current.offsetLeft - (nav.clientWidth - current.offsetWidth) / 2
      }
    }
    revealCurrent()
    window.addEventListener('resize', revealCurrent)
    return () => window.removeEventListener('resize', revealCurrent)
  }, [section.id])
  return (
    <div className="settings-page">
      <header className="settings-header">
        <span className="settings-eyebrow">{t('settingsBrand')}</span>
        <h1>{t('settings')}</h1>
        <p>{t('settingsIntro')}</p>
      </header>
      <div className="settings-layout">
        <nav
          ref={navRef}
          className="settings-nav"
          aria-label={t('settings')}
          onKeyDown={(event) => {
            const buttons = Array.from(event.currentTarget.querySelectorAll('button'))
            const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
            const next =
              event.key === 'ArrowDown' || event.key === 'ArrowRight'
                ? (index + 1) % buttons.length
                : event.key === 'ArrowUp' || event.key === 'ArrowLeft'
                  ? (index - 1 + buttons.length) % buttons.length
                  : event.key === 'Home'
                    ? 0
                    : event.key === 'End'
                      ? buttons.length - 1
                      : -1
            if (next >= 0) {
              event.preventDefault()
              buttons[next]?.focus()
            }
          }}
          style={
            {
              '--section': sections.indexOf(section),
              '--sections': sections.length,
            } as React.CSSProperties
          }
        >
          <div className="settings-nav-track">
            <span className="settings-nav-indicator" aria-hidden="true" />
            {sections.map(({ id, label, icon: Icon }) => (
              <button
                key={id}
                aria-current={section.id === id ? 'page' : undefined}
                onClick={() => void navigate({ to: '/settings', search: { section: id } })}
              >
                <Icon aria-hidden="true" />
                {t(label)}
              </button>
            ))}
          </div>
        </nav>
        <section className="settings-content" aria-label={t(section.label)} key={section.id}>
          {section.id !== 'usage' && <h2>{t(section.label)}</h2>}
          {section.id === 'general' ? (
            <General />
          ) : section.id === 'profile' ? (
            <Profile />
          ) : section.id === 'agents' ? (
            <AgentSettings />
          ) : section.id === 'connections' ? (
            <GoogleConnect />
          ) : section.id === 'passwords' ? (
            <Passwords />
          ) : section.id === 'skills' ? (
            <Skills />
          ) : (
            <Suspense
              fallback={
                <LoadingFallback>
                  <UsageSkeleton />
                </LoadingFallback>
              }
            >
              <Usage />
            </Suspense>
          )}
        </section>
      </div>
    </div>
  )
}
