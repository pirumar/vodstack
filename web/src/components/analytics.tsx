import { Globe2 } from 'lucide-react'
import type { AnalyticsRange, CountryStat } from '../api'
import { Card } from './ui'
import { formatDuration } from '../lib/format'

// Shared building blocks for the library + per-video stats screens.

const RANGES: { value: AnalyticsRange; label: string }[] = [
  { value: '7d', label: '7g' },
  { value: '30d', label: '30g' },
  { value: '90d', label: '90g' },
  { value: 'all', label: 'tümü' },
]

// RangeSelector is a small segmented control for the analytics time window.
export function RangeSelector({
  value,
  onChange,
}: {
  value: AnalyticsRange
  onChange: (r: AnalyticsRange) => void
}) {
  return (
    <div className="inline-flex rounded-lg border border-edge bg-panel/60 p-0.5">
      {RANGES.map((r) => (
        <button
          key={r.value}
          onClick={() => onChange(r.value)}
          className={`rounded-md px-3 py-1 font-mono text-[11px] uppercase tracking-[0.12em] transition ${
            value === r.value ? 'bg-signal/20 text-signal' : 'text-haze hover:text-chalk'
          }`}
        >
          {r.label}
        </button>
      ))}
    </div>
  )
}

// countryFlag maps an ISO-3166 alpha-2 code to its flag emoji (regional
// indicator symbols). Falls back to the raw code for non-letters.
export function countryFlag(code: string): string {
  const cc = code.toUpperCase()
  if (cc.length !== 2 || !/^[A-Z]{2}$/.test(cc)) return ''
  return String.fromCodePoint(...[...cc].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}

// CountryPanel renders the per-country breakdown: a ranked list with a bar
// proportional to starts, plus session count and watch time.
export function CountryPanel({ rows }: { rows: CountryStat[] }) {
  const max = Math.max(1, ...rows.map((r) => r.starts))
  return (
    <Card className="p-5">
      <div className="mb-4 flex items-center gap-2">
        <Globe2 className="h-4 w-4 text-signal" />
        <h3 className="font-display text-sm font-semibold">Ülke bazlı izlenme</h3>
      </div>
      {rows.length > 0 ? (
        <div className="space-y-2">
          {rows.map((c) => (
            <div key={c.country} className="flex items-center gap-3">
              <span className="w-14 shrink-0 font-mono text-xs text-chalk">
                {countryFlag(c.country)} {c.country}
              </span>
              <div className="relative h-5 flex-1 overflow-hidden rounded bg-ink/50">
                <div
                  className="absolute inset-y-0 left-0 rounded bg-signal/40"
                  style={{ width: `${Math.max(3, (c.starts / max) * 100)}%` }}
                />
              </div>
              <span className="w-16 shrink-0 text-right font-mono text-xs text-signal">{c.starts}</span>
              <span
                className="w-20 shrink-0 text-right font-mono text-[11px] text-haze"
                title={`${c.sessions} tekil izleyici`}
              >
                {formatDuration(Math.round(c.watchSeconds))}
              </span>
            </div>
          ))}
          <div className="flex items-center gap-3 pt-1 font-mono text-[9px] uppercase tracking-[0.18em] text-haze/60">
            <span className="w-14 shrink-0">ülke</span>
            <span className="flex-1">izlenme</span>
            <span className="w-16 shrink-0 text-right">başlatma</span>
            <span className="w-20 shrink-0 text-right">süre</span>
          </div>
        </div>
      ) : (
        <p className="font-mono text-[11px] text-haze">
          Henüz ülke verisi yok (yeni izlemelerden itibaren toplanır).
        </p>
      )}
    </Card>
  )
}
