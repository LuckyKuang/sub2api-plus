import { cn } from '@/lib/utils'

export function Card({ className, children, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('card surface-card', className)} {...rest}>
      {children}
    </div>
  )
}

export function CardHeader({ className, children, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('card-header', className)} {...rest}>
      {children}
    </div>
  )
}

export function CardBody({ className, children, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('card-body', className)} {...rest}>
      {children}
    </div>
  )
}

export function CardFooter({ className, children, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('card-footer', className)} {...rest}>
      {children}
    </div>
  )
}

export interface StatCardProps {
  icon?: React.ReactNode
  iconVariant?: 'primary' | 'success' | 'warning' | 'danger'
  label?: string
  value?: React.ReactNode
  trend?: React.ReactNode
  className?: string
}

const iconVariantClass = {
  primary: 'stat-icon-primary',
  success: 'stat-icon-success',
  warning: 'stat-icon-warning',
  danger: 'stat-icon-danger'
} as const

export function StatCard({ icon, iconVariant = 'primary', label, value, trend, className }: StatCardProps) {
  return (
    <Card className={cn('stat-card', className)}>
      {icon && <div className={cn('stat-icon', iconVariantClass[iconVariant])}>{icon}</div>}
      <div className="min-w-0 flex-1">
        {label && <p className="stat-label">{label}</p>}
        <p className="stat-value">{value}</p>
        {trend && <div className="stat-trend">{trend}</div>}
      </div>
    </Card>
  )
}
