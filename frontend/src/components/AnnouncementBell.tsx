import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { Modal, Spinner } from './ui'
import { useAppStore } from '@/stores/app'
import { useAnnouncementsStore } from '@/stores/announcements'
import { formatRelativeTime, formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import { Icon } from './Icon'
import { useSupportReadonly } from '@/composables/useSupportReadonly'
import '@/styles/announcement-markdown.css'

marked.setOptions({
  breaks: true,
  gfm: true
})

function renderMarkdown(content: string): string {
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
}

export interface AnnouncementBellProps {
  className?: string
}

export function AnnouncementBell({ className }: AnnouncementBellProps) {
  const { t } = useTranslation()
  const showError = useAppStore((s) => s.showError)
  const showSuccess = useAppStore((s) => s.showSuccess)
  const announcements = useAnnouncementsStore((s) => s.announcements)
  const loading = useAnnouncementsStore((s) => s.loading)
  const markAsReadAction = useAnnouncementsStore((s) => s.markAsRead)
  const markAllAsReadAction = useAnnouncementsStore((s) => s.markAllAsRead)
  const unreadCount = announcements.filter((item) => !item.read_at).length

  // Local modal state
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [detailModalOpen, setDetailModalOpen] = useState(false)
  const [selectedAnnouncement, setSelectedAnnouncement] = useState<UserAnnouncement | null>(null)
  const markAllRef = useRef<HTMLButtonElement | null>(null)
  useSupportReadonly(markAllRef)

  const openModal = () => setIsModalOpen(true)
  const closeModal = () => setIsModalOpen(false)

  const openDetail = (announcement: UserAnnouncement) => {
    setSelectedAnnouncement(announcement)
    setDetailModalOpen(true)
    if (!announcement.read_at) {
      void markAsRead(announcement.id)
    }
  }

  const closeDetail = () => {
    setDetailModalOpen(false)
    setSelectedAnnouncement(null)
  }

  const markAsRead = async (id: number) => {
    try {
      const marked = await markAsReadAction(id)
      if (!marked) showError(t('common.unknownError'))
      return marked
    } catch (err: unknown) {
      const message = (err as { message?: string })?.message || t('common.unknownError')
      showError(message)
      return false
    }
  }

  const markAsReadAndClose = async (id: number) => {
    if (!(await markAsRead(id))) return
    showSuccess(t('announcements.markedAsRead'))
    closeDetail()
  }

  const markAllAsRead = async () => {
    try {
      await markAllAsReadAction()
      showSuccess(t('announcements.allMarkedAsRead'))
    } catch (err: unknown) {
      const message = (err as { message?: string })?.message || t('common.unknownError')
      showError(message)
    }
  }

  return (
    <div className={className}>
      {/* 铃铛按钮 */}
      <button
        onClick={openModal}
        className="relative flex h-9 w-9 items-center justify-center rounded-lg transition-colors hover:surface-hover surface-control surface-muted"
        aria-label={t('announcements.title')}
      >
        <Icon name="bell" size="md" />
        {/* 未读标记 */}
        {unreadCount > 0 && (
          <span className="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-rose-500"></span>
        )}
      </button>

      {/* 公告列表 */}
      <Modal
        open={isModalOpen}
        title={t('announcements.title')}
        size="md"
        onClose={closeModal}
      >
        {unreadCount > 0 && (
          <div className="mb-4 flex items-center justify-between gap-3">
            <p className="text-sm surface-muted">
              <span className="font-medium">{unreadCount}</span> {t('announcements.unread')}
            </p>
            <button
              ref={markAllRef}
              onClick={() => void markAllAsRead()}
              disabled={loading}
              className="btn btn-secondary text-xs"
            >
              {t('announcements.markAllRead')}
            </button>
          </div>
        )}

        {/* Loading */}
        {loading ? (
          <div className="flex items-center justify-center py-16">
            <Spinner />
          </div>
        ) : announcements.length > 0 ? (
          /* Announcements List */
          <div className="-mx-6 -my-4">
            {announcements.map((item) => (
              <button
                key={item.id}
                type="button"
                className="group relative flex w-full items-center gap-4 border-b border-gray-100 px-6 py-4 text-left transition-colors last:border-b-0 hover:surface-hover dark:border-dark-700"
                style={{ minHeight: '72px' }}
                data-testid="announcement-row"
                onClick={() => openDetail(item)}
              >
                {/* Status Indicator */}
                <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-[var(--surface-radius-item)] surface-inset surface-muted">
                  <Icon name={!item.read_at ? 'infoCircle' : 'check'} size="sm" />
                </div>

                {/* Content */}
                <div className="flex min-w-0 flex-1 items-center justify-between gap-4">
                  <div className="min-w-0 flex-1">
                    <h3 className="truncate text-sm font-medium text-gray-900 dark:text-white">
                      {item.title}
                    </h3>
                    <div className="mt-1 flex items-center gap-2">
                      <time className="text-xs surface-muted">{formatRelativeTime(item.created_at)}</time>
                      {!item.read_at && (
                        <span className="inline-flex items-center gap-1 rounded-md surface-inset px-1.5 py-0.5 text-xs font-medium surface-muted">
                          <span className="console-data-mark h-1.5 w-1.5 rounded-full bg-rose-500"></span>
                          {t('announcements.unread')}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Arrow */}
                  <Icon name="chevronRight" size="sm" className="flex-shrink-0 surface-muted" />
                </div>
              </button>
            ))}
          </div>
        ) : (
          /* Empty State */
          <div className="flex flex-col items-center justify-center py-16">
            <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-[var(--surface-radius-panel)] surface-inset surface-muted">
              <Icon name="inbox" size="lg" />
            </div>
            <p className="text-sm font-medium text-gray-900 dark:text-white">{t('announcements.empty')}</p>
            <p className="mt-1 text-xs surface-muted">{t('announcements.emptyDescription')}</p>
          </div>
        )}
      </Modal>

      {/* 公告详情 */}
      <Modal
        open={detailModalOpen && !!selectedAnnouncement}
        title={selectedAnnouncement?.title || ''}
        size="xl"
        onClose={closeDetail}
        footer={
          <>
            <span className="mr-auto text-xs surface-muted">
              {selectedAnnouncement?.read_at
                ? t('announcements.readStatus')
                : t('announcements.markReadHint')}
            </span>
            <button onClick={closeDetail} className="btn btn-secondary">
              {t('common.close')}
            </button>
            {selectedAnnouncement && !selectedAnnouncement.read_at && (
              <button onClick={() => void markAsReadAndClose(selectedAnnouncement.id)} className="btn btn-primary">
                <span className="flex items-center gap-2">
                  <Icon name="check" size="sm" />
                  {t('announcements.markRead')}
                </span>
              </button>
            )}
          </>
        }
      >
        <p className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm surface-muted">
          <span className="flex items-center gap-1.5">
            <Icon name="clock" size="sm" />
            <time>
              {selectedAnnouncement ? formatRelativeWithDateTime(selectedAnnouncement.created_at) : ''}
            </time>
          </span>
          <span className="flex items-center gap-1.5">
            <Icon name="eye" size="sm" />
            {selectedAnnouncement?.read_at ? t('announcements.read') : t('announcements.unread')}
          </span>
        </p>
        <div
          className="markdown-body prose prose-sm max-w-none dark:prose-invert"
          dangerouslySetInnerHTML={{
            __html: selectedAnnouncement ? renderMarkdown(selectedAnnouncement.content) : ''
          }}
        ></div>
      </Modal>
    </div>
  )
}

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>

export interface AnnouncementPopupProps {
  announcement?: PreviewAnnouncement | null
  preview?: boolean
  onClose?: () => void
}

export function AnnouncementPopup({ announcement = null, preview = false, onClose }: AnnouncementPopupProps) {
  const { t } = useTranslation()
  const storePopup = useAnnouncementsStore((s) => s.currentPopup)
  const dismissPopupAction = useAnnouncementsStore((s) => s.dismissPopup)

  const displayedAnnouncement = preview ? announcement : storePopup

  const renderedContent = (() => {
    const content = displayedAnnouncement?.content
    if (!content) return ''
    const html = marked.parse(content) as string
    return DOMPurify.sanitize(html)
  })()

  const handleDismiss = () => {
    if (preview) {
      onClose?.()
      return
    }
    void dismissPopupAction()
  }

  return (
    <Modal
      open={!!displayedAnnouncement}
      title={displayedAnnouncement?.title || ''}
      size="xl"
      showClose
      onClose={handleDismiss}
      footer={
        <button
          onClick={handleDismiss}
          data-testid="announcement-popup-dismiss"
          className="btn btn-primary"
        >
          <span className="flex items-center gap-2">
            <Icon name={preview ? 'x' : 'check'} size="sm" />
            {preview ? t('common.close') : t('announcements.markRead')}
          </span>
        </button>
      }
    >
      <p className="mb-4 flex items-center gap-1.5 text-sm surface-muted">
        <Icon name="clock" size="sm" />
        <time>{displayedAnnouncement ? formatRelativeWithDateTime(displayedAnnouncement.created_at) : ''}</time>
      </p>
      <div
        className="markdown-body prose prose-sm max-w-none dark:prose-invert"
        dangerouslySetInnerHTML={{ __html: renderedContent }}
      ></div>
    </Modal>
  )
}
