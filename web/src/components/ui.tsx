import { type ReactNode } from 'react'

// Shared layout primitives so every page shares the same broadcast-deck rhythm:
// consistent cards, page headers, empty states, and stat tiles.

export function Card({
  children,
  className = '',
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div className={`rounded-2xl border border-edge bg-panel/60 shadow-deck ${className}`}>
      {children}
    </div>
  )
}

export function PageHeader({
  eyebrow,
  title,
  count,
  icon,
  actions,
}: {
  eyebrow?: string
  title: string
  count?: ReactNode
  icon?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="mb-7 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0">
        {eyebrow && <div className="eyebrow mb-1.5">{eyebrow}</div>}
        <div className="flex items-center gap-3">
          {icon && <span className="text-signal">{icon}</span>}
          <h1 className="font-display text-2xl font-semibold tracking-tight">{title}</h1>
          {count !== undefined && <span className="font-mono text-sm text-haze">/ {count}</span>}
        </div>
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}

export function EmptyState({
  title,
  hint,
  icon,
}: {
  title: string
  hint?: string
  icon?: ReactNode
}) {
  return (
    <div className="grid place-items-center rounded-2xl border border-dashed border-edge bg-panel/30 py-20 text-center">
      {icon && <div className="mb-3 text-haze/60">{icon}</div>}
      <p className="font-display text-lg font-semibold text-haze">{title}</p>
      {hint && (
        <p className="mt-1 font-mono text-[11px] uppercase tracking-[0.18em] text-haze/60">{hint}</p>
      )}
    </div>
  )
}

export function StatTile({
  label,
  value,
  tone,
  icon,
}: {
  label: string
  value: ReactNode
  tone?: 'ok' | 'signal' | 'bad' | 'warn'
  icon?: ReactNode
}) {
  const color =
    tone === 'ok'
      ? 'text-ok'
      : tone === 'signal'
        ? 'text-signal'
        : tone === 'bad'
          ? 'text-bad'
          : tone === 'warn'
            ? 'text-warn'
            : 'text-chalk'
  return (
    <Card className="p-4">
      <div className="flex items-center justify-between">
        <div className="eyebrow">{label}</div>
        {icon && <span className="text-haze/70">{icon}</span>}
      </div>
      <div className={`mt-2 font-mono text-2xl font-semibold ${color}`}>{value}</div>
    </Card>
  )
}
