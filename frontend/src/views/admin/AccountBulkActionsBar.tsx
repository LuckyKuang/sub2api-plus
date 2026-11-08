import { useTranslation } from 'react-i18next'

export interface AccountBulkActionsBarProps {
  selectedIds: number[]
  totalResults: number
  selectingAll: boolean
  allResultsSelected: boolean
  onDelete: () => void
  onResetStatus: () => void
  onRefreshToken: () => void
  onEditSelected: () => void
  onEditFiltered: () => void
  onClear: () => void
  onSelectPage: () => void
  onSelectAllResults: () => void
  onToggleSchedulable: (schedulable: boolean) => void
}

export function AccountBulkActionsBar({
  selectedIds,
  totalResults,
  selectingAll,
  allResultsSelected,
  onDelete,
  onResetStatus,
  onRefreshToken,
  onEditSelected,
  onEditFiltered,
  onClear,
  onSelectPage,
  onSelectAllResults,
  onToggleSchedulable
}: AccountBulkActionsBarProps) {
  const { t } = useTranslation()
  return (
    <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-lg bg-primary-50/[0.15] p-3 dark:bg-primary-900/20">
      <div className="flex flex-wrap items-center gap-2">
        {allResultsSelected ? (
          <span className="text-sm font-medium text-primary-900 dark:text-primary-100">
            {t('admin.accounts.bulkActions.selectedAll', { count: selectedIds.length })}
          </span>
        ) : selectedIds.length > 0 ? (
          <span className="text-sm font-medium text-primary-900 dark:text-primary-100">
            {t('admin.accounts.bulkActions.selected', { count: selectedIds.length })}
          </span>
        ) : (
          <span className="text-sm font-medium text-primary-900 dark:text-primary-100">
            {t('admin.accounts.bulkEdit.title')}
          </span>
        )}
        {selectedIds.length > 0 && (
          <button
            onClick={onSelectPage}
            className="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200 surface-control"
          >
            {t('admin.accounts.bulkActions.selectCurrentPage')}
          </button>
        )}
        {!allResultsSelected && totalResults > selectedIds.length && (
          <>
            {selectedIds.length > 0 && <span className="text-gray-300 dark:text-primary-800">•</span>}
            <button
              disabled={selectingAll}
              onClick={onSelectAllResults}
              className="text-xs font-medium text-primary-700 hover:text-primary-800 disabled:cursor-not-allowed disabled:opacity-60 dark:text-primary-300 dark:hover:text-primary-200 surface-control"
            >
              {selectingAll
                ? t('admin.accounts.bulkActions.selectingAll')
                : t('admin.accounts.bulkActions.selectAllResults', { count: totalResults })}
            </button>
          </>
        )}
        {selectedIds.length > 0 && (
          <>
            <span className="text-gray-300 dark:text-primary-800">•</span>
            <button
              onClick={onClear}
              className="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200 surface-control"
            >
              {t('admin.accounts.bulkActions.clear')}
            </button>
          </>
        )}
      </div>
      <div className="flex gap-2">
        {selectedIds.length > 0 && (
          <>
            <button onClick={onDelete} className="btn btn-danger btn-sm">{t('admin.accounts.bulkActions.delete')}</button>
            <button onClick={onResetStatus} className="btn btn-secondary btn-sm">{t('admin.accounts.bulkActions.resetStatus')}</button>
            <button onClick={onRefreshToken} className="btn btn-secondary btn-sm">{t('admin.accounts.bulkActions.refreshToken')}</button>
            <button onClick={() => onToggleSchedulable(true)} className="btn btn-success btn-sm">{t('admin.accounts.bulkActions.enableScheduling')}</button>
            <button onClick={() => onToggleSchedulable(false)} className="btn btn-warning btn-sm">{t('admin.accounts.bulkActions.disableScheduling')}</button>
            <button onClick={onEditSelected} className="btn btn-primary btn-sm">{t('admin.accounts.bulkActions.edit')}</button>
          </>
        )}
        <button onClick={onEditFiltered} className="btn btn-primary btn-sm">
          {t('admin.accounts.bulkEdit.submit')}
        </button>
      </div>
    </div>
  )
}

export default AccountBulkActionsBar
