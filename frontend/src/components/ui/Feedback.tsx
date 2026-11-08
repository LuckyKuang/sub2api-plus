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
      {icon && <div className="empty-state-icon">{icon}</div>}
      {title && <h3 className="empty-state-title">{title}</h3>}
      {description && <p className="empty-state-description">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}
