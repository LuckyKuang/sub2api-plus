import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { Button, Modal } from '@/components/ui'
import { useAnnouncementsStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import { Icon } from '@/views/admin/AccountIcon'

marked.setOptions({ breaks: true, gfm: true })

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>

export interface AnnouncementPopupProps {
  announcement?: PreviewAnnouncement | null
  preview?: boolean
  onClose?: () => void
}

/** Announcement popup ported from the Vue AnnouncementPopup. */
export function AnnouncementPopup({ announcement = null, preview = false, onClose }: AnnouncementPopupProps) {
  const { t } = useTranslation()
  const currentPopup = useAnnouncementsStore((s) => s.currentPopup)
  const dismissPopup = useAnnouncementsStore((s) => s.dismissPopup)

  const displayedAnnouncement = preview ? announcement : currentPopup

  const renderedContent = useMemo(() => {
    const content = displayedAnnouncement?.content
    if (!content) return ''
    const html = marked.parse(content) as string
    return DOMPurify.sanitize(html)
  }, [displayedAnnouncement])

  function handleDismiss() {
    if (preview) {
      onClose?.()
      return
    }
    dismissPopup()
  }

  return (
    <Modal
      open={Boolean(displayedAnnouncement)}
      onClose={handleDismiss}
      title={displayedAnnouncement?.title || ''}
      size="lg"
      footer={
        <Button variant="primary" onClick={handleDismiss} data-testid="announcement-popup-dismiss">
          <span className="flex items-center gap-2">
            {preview ? <Icon name="x" size="sm" /> : <Icon name="check" size="sm" />}
            {preview ? t('common.close') : t('announcements.markRead')}
          </span>
        </Button>
      }
    >
      <p className="mb-4 flex items-center gap-1.5 text-sm surface-muted">
        <Icon name="clock" size="sm" />
        <time>{displayedAnnouncement ? formatRelativeWithDateTime(displayedAnnouncement.created_at) : ''}</time>
      </p>
      <div
        className="markdown-body prose prose-sm max-w-none dark:prose-invert"
        // Sanitized with DOMPurify above; mirrors the Vue v-html contract.
        dangerouslySetInnerHTML={{ __html: renderedContent }}
      />
    </Modal>
  )
}

export default AnnouncementPopup
