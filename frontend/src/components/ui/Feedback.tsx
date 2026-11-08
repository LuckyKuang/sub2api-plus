import { cn } from '@/lib/utils'

type BadgeVariant = 'primary' | 'success' | 'warning' | 'danger' | 'gray' | 'purple'

const variantClass: Record<BadgeVariant, string> = {
  primary: 'badge-primary',
  success: 'badge-success',
  warning: 'badge-warning',
  danger: 'badge-danger',
  gray: 'badge-gray',
  purple: 'badge-purple'
}

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant
}

export function Badge({ variant = 'gray', className, children, ...rest }: BadgeProps) {
  return (
    <span className={cn('badge', variantClass[variant], className)} {...rest}>
      {children}
    </span>
  )
}

export function Spinner({ className }: { className?: string }) {
  return <span className={cn('spinner', className)} aria-hidden="true" />
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn('skeleton', className)} aria-hidden="true" />
}

export interface LoadingStateProps {
  label?: string
  className?: string
}

export function LoadingState({ label, className }: LoadingStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-3 py-12', className)}>
      <Spinner className="h-6 w-6 text-primary-500" />
      {label && <p className="text-sm text-gray-500 dark:text-dark-400">{label}</p>}
    </div>
  )
}

export interface EmptyStateProps {
  icon?: React.ReactNode
  title?: string
  description?: string
  action?: React.ReactNode
  className?: string
}

export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn('empty-state', className)}>
      {/* Vue wraps the glyph in a decorative inset tile and falls back to the
          inbox icon, so a caller passing no icon still matches. */}
      <div className="mb-5 flex h-20 w-20 items-center justify-center rounded-2xl surface-inset surface-decoration">
        {icon ?? (
          <svg
            className="empty-state-icon h-10 w-10"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
            strokeWidth="1.5"
            aria-hidden="true"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"
            />
          </svg>
        )}
      </div>
      {title && <h3 className="empty-state-title">{title}</h3>}
      {description && <p className="empty-state-description">{description}</p>}
      {action && <div className="mt-6">{action}</div>}
    </div>
  )
}
