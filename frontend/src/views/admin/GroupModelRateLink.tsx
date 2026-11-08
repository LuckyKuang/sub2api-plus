import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Select } from '@/components/ui'

export function GroupModelRateLink({ groups }: { groups: Array<{ id: number; name: string }> }) {
  const { t } = useTranslation()
  const [selectedGroup, setSelectedGroup] = useState<number | null>(null)

  const options = groups.map((group) => ({ value: group.id, label: group.name }))

  useEffect(() => {
    if (!groups.some((group) => group.id === selectedGroup)) {
      setSelectedGroup(groups.length === 1 ? groups[0].id : null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groups])

  return (
    <div className="mt-3 space-y-2 rounded-lg surface-inset p-3">
      <p className="text-xs surface-muted">{t('billingRates.editor.sharedPriceHint')}</p>
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={selectedGroup}
          options={options}
          placeholder={t('billingRates.editor.selectBoundGroup')}
          onValueChange={(value) => setSelectedGroup(typeof value === 'number' ? value : null)}
          className="min-w-0 flex-1"
          searchable
        />
        {selectedGroup && (
          <Link
            to={{ pathname: '/admin/groups', search: `?edit_group_id=${selectedGroup}` }}
            target="_blank"
            rel="noopener"
            className="btn btn-secondary btn-sm"
          >
            {t('billingRates.editor.openGroupRates')}
          </Link>
        )}
      </div>
    </div>
  )
}

export default GroupModelRateLink
