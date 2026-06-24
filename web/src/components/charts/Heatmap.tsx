import { formatDuration } from '../../lib/format'

// Renders a normalized [0,1] watchtime curve as an engagement heatmap, mirroring
// the player's heatmap. `buckets` are evenly spread across `duration` seconds.

export function Heatmap({
  buckets,
  duration,
}: {
  buckets: number[]
  duration?: number
}) {
  return (
    <div>
      <div className="flex h-24 items-end gap-px overflow-hidden rounded-lg border border-edge bg-ink/60 p-1">
        {buckets.map((v, i) => (
          <div
            key={i}
            className="min-w-0 flex-1 rounded-sm bg-gradient-to-t from-signal/30 to-signal"
            style={{ height: `${Math.max(3, v * 100)}%`, opacity: 0.35 + v * 0.65 }}
            title={
              duration
                ? `${formatDuration(Math.round((i / buckets.length) * duration))} · ${Math.round(v * 100)}%`
                : `${Math.round(v * 100)}%`
            }
          />
        ))}
      </div>
      {duration ? (
        <div className="mt-1.5 flex justify-between font-mono text-[10px] text-haze/60">
          <span>0:00</span>
          <span>{formatDuration(duration)}</span>
        </div>
      ) : null}
    </div>
  )
}
