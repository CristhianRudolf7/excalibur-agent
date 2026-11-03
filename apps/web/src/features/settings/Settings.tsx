import { AgentSettings } from '@/features/agents/AgentSettings'
import { useArtifacts } from '@/features/artifacts/store'
import { loadingLazy as lazy, LoadingFallback } from '@/features/shell/loading'
import { useLoading } from '@/components/shared'
import { UsageSkeleton } from './Skeletons'
import { useEffect, useState, Suspense } from 'react'
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
      <label>{t('settingsLanguage')}</label>
      <p>{t('settingsLanguageNote')}</p>
      <Segmented
        label={t('settingsLanguage')}
        value={i18n.language.startsWith('en') ? 'en' : 'es'}
        options={[
          { value: 'es', label: t('settingsSpanish') },
          { value: 'en', label: t('settingsEnglish') },
        ]}
        onChange={(value) => void i18n.changeLanguage(value)}
      />
      <label>
        <input
          type="checkbox"
          checked={autoOpen}
          onChange={(event) => setAutoOpen(event.target.checked)}
        />
        {t('artifactAutoOpen')}
      </label>
      <TimeZone />
      <label>{t('settingsTheme')}</label>
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
  )
}
function Profile() {
  const { t } = useTranslation()
  const profile = useProfile()
  const loading = useLoading(profile.isPending)
  return (
    <div className="settings-card">
      <Feedback loading={loading} query={profile} />
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
  return (
    <div className="settings-page">
      <header className="settings-header">
        <span className="settings-eyebrow">{t('settingsBrand')}</span>
        <h1>{t('settings')}</h1>
        <p>{t('settingsIntro')}</p>
      </header>
      <div className="settings-layout">
        <nav
          className="settings-nav"
          aria-label={t('settings')}
          style={
            {
              '--section': sections.indexOf(section),
              '--sections': sections.length,
            } as React.CSSProperties
          }
        >
          <span className="settings-nav-indicator" aria-hidden="true" />
          {sections.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              aria-current={section.id === id ? 'page' : undefined}
              onClick={() => void navigate({ to: '/settings', search: { section: id } })}
            >
              <Icon />
              {t(label)}
            </button>
          ))}
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
