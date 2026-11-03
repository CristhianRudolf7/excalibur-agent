import { CollectionIcon } from './CollectionIcon'
import { useState } from 'react'
import { Database, ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Collection } from '@/lib/rpc/contract'
import './mydata.css'

export function CollectionNav({
  collections,
  active,
  onNavigate,
  onAsk,
}: {
  collections: Collection[]
  active: string
  onNavigate: (key: string) => void
  onAsk: () => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(true)
  return (
    <nav className="secondary-nav data-nav" aria-label={t('myData')}>
      <div className="data-nav-heading">
        <button
          className={active.startsWith('/data') ? 'selected' : ''}
          onClick={() => onNavigate('myData')}
          aria-current={active === '/data' ? 'page' : undefined}
        >
          <Database />
          <span className="sidebar-label">{t('myData')}</span>
        </button>
        <button
          className="data-collapse"
          aria-expanded={open}
          aria-label={t(open ? 'dataCollapse' : 'dataExpand')}
          onClick={() => setOpen(!open)}
        >
          <ChevronDown size={14} />
        </button>
      </div>
      {open && (
        <div className="installed-extension-nav" role="group" aria-label={t('myData')}>
          {collections.length ? (
            collections.map((collection) => (
              <button
                key={collection.id}
                className={
                  active.startsWith(`/data/${encodeURIComponent(collection.id)}`) ? 'selected' : ''
                }
                onClick={() => onNavigate(`data:${collection.id}`)}
              >
                <CollectionIcon name={collection.icon} size={16} />
                <span className="sidebar-label">{collection.name}</span>
                <span className="data-count">{collection.record_count}</span>
              </button>
            ))
          ) : (
            <button className="data-nav-empty" onClick={onAsk}>
              {t('dataAsk')}
            </button>
          )}
        </div>
      )}
    </nav>
  )
}
