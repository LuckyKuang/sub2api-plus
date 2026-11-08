import { useTranslation } from 'react-i18next'
import type { AutoRefreshState } from '@/views/user/useAutoRefresh'
import { AutoRefreshButton } from './AutoRefreshButton'

export interface RefreshControlProps {
  state: AutoRefreshState
  manual?: boolean
  switchVisible?: boolean
}

export function RefreshControl({ state, manual = false, switchVisible = false }: RefreshControlProps) {
  const { t } = useTranslation()

  return (
    <div className="flex flex-wrap items-center gap-2 text-xs surface-muted">
      {switchVisible && (
        <button
          type="button"
          role="switch"
          aria-checked={state.enabled}
          className="surface-control inline-flex min-h-9 items-center gap-2 rounded-lg border border-gray-200 px-3 dark:border-dark-600"
          onClick={() => state.setEnabled(!state.enabled)}
        >
          <span
            className={`refresh-switch-track h-4 w-7 rounded-full transition-colors ${state.enabled ? 'bg-primary-500' : 'bg-gray-400'}`}
          >
            <span
              className={`block h-3 w-3 rounded-full surface-opaque transition-transform ${state.enabled ? 'translate-x-3' : ''}`}
            />
          </span>
          {t('common.autoRefresh.title')}
        </button>
      )}
      <AutoRefreshButton
        intervalOnly={switchVisible}
        enabled={state.enabled}
        intervalSeconds={state.intervalSeconds}
        countdown={state.countdown}
        fetching={state.fetching}
        paused={state.paused}
        intervals={state.intervals}
        onEnabledChange={state.setEnabled}
        onIntervalChange={state.setInterval}
      />
      {manual && (
        <button type="button" className="btn btn-secondary" disabled={state.fetching} onClick={() => void state.refresh()}>
          {t('common.refresh')}
        </button>
      )}
      {state.error ? (
        <span role="status" className="text-amber-600">
          {t('inspection.stale')}
        </span>
      ) : state.lastUpdated ? (
        <span>{t('inspection.updatedAt', { time: new Date(state.lastUpdated).toLocaleTimeString() })}</span>
      ) : null}
    </div>
  )
}

export default RefreshControl
