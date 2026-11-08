import { useTranslation } from 'react-i18next'
import { Card, EmptyState } from '@/components/ui'
import { Construction } from 'lucide-react'

/**
 * Channel monitor V2 (ops-style monitor) user view.
 *
 * The Vue screen composes `FilterMultiSelect`, `MetricCell`,
 * `MonitorRankBadge`, `MonitorTrendChart` and `RelayPulseMatrix` from
 * `src/features/channel-monitor-v2`, which is owned by another workstream and
 * is not ported yet. This file keeps the route importable so the channel-status
 * dispatcher can select V2; replace the placeholder once the feature module
 * lands.
 */
export default function ChannelStatusV2View() {
  const { t } = useTranslation()

  return (
    <Card className="p-6">
      <EmptyState
        icon={<Construction className="h-10 w-10" />}
        title={t('channelMonitorV2.title')}
        description={t('common.portInProgress')}
      />
    </Card>
  )
}
