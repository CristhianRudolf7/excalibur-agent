import { useState, type CSSProperties } from 'react'
import { useNavigate } from '@tanstack/react-router'
import {
  ArrowUpRight,
  Check,
  Download,
  GraduationCap,
  Puzzle,
  Search,
  Sparkles,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  extensionCatalog,
  extensionCategories,
  type Extension,
  type ExtensionCategory,
} from './catalog'
import { useExtensions } from './store'
import './extensions.css'

function searchable(value: string) {
  return value
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLocaleLowerCase()
    .trim()
}

export function ExtensionsPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { installed, setInstalled } = useExtensions()
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState<ExtensionCategory | 'all'>('all')
  const [view, setView] = useState<'installed' | 'available'>('installed')
  const [notice, setNotice] = useState('')
  const changeInstallation = (extension: Extension, next: boolean) => {
    setInstalled(extension.id, next)
    setNotice(
      t(next ? 'extensionsInstalledNotice' : 'extensionsUninstalledNotice', {
        name: t(extension.nameKey),
      }),
    )
  }
  const inView = extensionCatalog.filter(
    ({ id }) => installed.includes(id) === (view === 'installed'),
  )
  const filtered = inView.filter(
    (extension) =>
      (category === 'all' || extension.category === category) &&
      searchable(
        `${t(extension.nameKey)} ${t(extension.descriptionKey)} ${t(`extensionsCategory_${extension.category}`)}`,
      ).includes(searchable(query)),
  )
  const featured = extensionCatalog[0]!
  const featuredInstalled = installed.includes(featured.id)
  const hasFilters = query.trim() !== '' || category !== 'all'

  return (
    <div className="extensions-page">
      <header className="extensions-header">
        <span className="extensions-eyebrow">
          <Puzzle size={16} aria-hidden="true" />
          {t('workspace')}
        </span>
        <h1>{t('extensions')}</h1>
        <p>{t('extensionsSubtitle')}</p>
      </header>
      <section className="extensions-hero" aria-labelledby="extensions-featured-title">
        <div className="extensions-hero-copy">
          <span className="extensions-eyebrow">
            <Sparkles size={15} aria-hidden="true" />
            {t('extensionsFeatured')} · {t(featured.nameKey)}
          </span>
          <h2 id="extensions-featured-title">{t('extensionsHeroTitle')}</h2>
          <p>{t('extensionsHeroDescription')}</p>
          <Button
            variant="primary"
            onClick={() =>
              featuredInstalled
                ? void navigate({ to: featured.route })
                : changeInstallation(featured, true)
            }
          >
            {t(featuredInstalled ? 'extensionsOpen' : 'extensionsInstall')}
            <ArrowUpRight size={16} aria-hidden="true" />
          </Button>
        </div>
        <div className="extensions-hero-art" aria-hidden="true">
          <div className="extensions-orbit extensions-orbit-outer" />
          <div className="extensions-orbit extensions-orbit-inner" />
          <div className="extensions-hero-icon">
            <GraduationCap strokeWidth={1.3} />
          </div>
          <span className="extensions-hero-spark">
            <Sparkles />
          </span>
          <span className="extensions-hero-check">
            <Check />
          </span>
        </div>
      </section>
      <div className="extensions-discovery">
        <label className="extensions-search">
          <Search size={19} aria-hidden="true" />
          <input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            aria-label={t('extensionsSearch')}
            placeholder={t('extensionsSearchPlaceholder')}
          />
        </label>
        <div className="extensions-categories" role="group" aria-label={t('extensionsCategories')}>
          {(['all', ...extensionCategories] as const).map((item) => (
            <button
              key={item}
              type="button"
              aria-pressed={category === item}
              onClick={() => setCategory(item)}
            >
              {t(item === 'all' ? 'extensionsAll' : `extensionsCategory_${item}`)}
            </button>
          ))}
        </div>
      </div>
      <div className="extensions-views" role="group" aria-label={t('extensionsViews')}>
        {(['installed', 'available'] as const).map((item) => (
          <button
            type="button"
            key={item}
            aria-pressed={view === item}
            onClick={() => setView(item)}
          >
            {t(item === 'installed' ? 'extensionsInstalled' : 'extensionsAvailable')}{' '}
            <span>
              {item === 'installed' ? installed.length : extensionCatalog.length - installed.length}
            </span>
          </button>
        ))}
      </div>
      <p className="extensions-notice" role="status">
        {notice}
      </p>
      <section
        className="extensions-results"
        aria-label={t(view === 'installed' ? 'extensionsInstalled' : 'extensionsAvailable')}
      >
        {filtered.length ? (
          <div className="extensions-grid" key={`${view}-${category}-${query}`}>
            {filtered.map((extension, index) => {
              const Icon = extension.icon
              const isInstalled = installed.includes(extension.id)
              return (
                <article
                  className="extension-card"
                  key={extension.id}
                  style={{ '--extension-index': index } as CSSProperties}
                  aria-labelledby={`extension-${extension.id}`}
                >
                  <div className="extension-card-top">
                    <span className="extension-icon">
                      <Icon aria-hidden="true" />
                    </span>
                    <span className="extension-builtin">
                      {isInstalled && <Check size={12} aria-hidden="true" />}
                      {t('extensionsBuiltin')}
                    </span>
                  </div>
                  <span className="extension-category">
                    {t(`extensionsCategory_${extension.category}`)}
                  </span>
                  <h2 id={`extension-${extension.id}`}>{t(extension.nameKey)}</h2>
                  <p>{t(extension.descriptionKey)}</p>
                  <div className="extension-actions">
                    {isInstalled && (
                      <Button
                        variant="primary"
                        onClick={() => void navigate({ to: extension.route })}
                      >
                        {t('extensionsOpen')}
                        <ArrowUpRight size={15} aria-hidden="true" />
                      </Button>
                    )}
                    <Button
                      variant={isInstalled ? 'ghost' : 'primary'}
                      onClick={() => changeInstallation(extension, !isInstalled)}
                    >
                      {!isInstalled && <Download size={15} aria-hidden="true" />}
                      {t(isInstalled ? 'extensionsUninstall' : 'extensionsInstall')}
                    </Button>
                  </div>
                </article>
              )
            })}
          </div>
        ) : (
          <div className="extensions-empty" key={`${view}-${category}-${query}`}>
            <span className="extension-icon">
              <Puzzle aria-hidden="true" />
            </span>
            <h2>
              {t(
                hasFilters
                  ? 'extensionsNoResults'
                  : view === 'available'
                    ? 'extensionsEmptyTitle'
                    : 'extensionsNoInstalled',
              )}
            </h2>
            <p>
              {t(
                hasFilters
                  ? 'extensionsNoResultsDescription'
                  : view === 'available'
                    ? 'extensionsEmptyDescription'
                    : 'extensionsNoInstalledDescription',
              )}
            </p>
            {hasFilters && (
              <Button
                variant="secondary"
                onClick={() => {
                  setQuery('')
                  setCategory('all')
                }}
              >
                {t('extensionsClearFilters')}
              </Button>
            )}
          </div>
        )}
      </section>
    </div>
  )
}
