import { useTranslation } from 'react-i18next'
import { Construction } from 'lucide-react'
import { Card, EmptyState } from '@/components/ui'

export interface PlaceholderViewProps {
  titleKey?: string
  fallbackTitle?: string
}

/**
 * Temporary stand-in for a view that has not been ported yet. The route is
 * registered and navigable so the shell, guards and titles can be verified
 * end-to-end while the real screen is being ported.
 */
export function PlaceholderView({ titleKey, fallbackTitle }: PlaceholderViewProps) {
  const { t } = useTranslation()
  const title = (titleKey && t(titleKey)) || fallbackTitle || ''

  return (
    <Card className="p-6">
      <EmptyState
        icon={<Construction className="h-10 w-10" />}
        title={title}
        description={t('common.portInProgress', 'This screen is being ported to the new interface.')}
      />
    </Card>
  )
}

export default PlaceholderView
