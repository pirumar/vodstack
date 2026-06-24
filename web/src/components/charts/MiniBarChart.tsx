// Dependency-free bar chart that matches the deck aesthetic. Renders labelled
// vertical bars from a series of {label, value} points.

export interface BarPoint {
  label: string
  value: number
  hint?: string
}

export function MiniBarChart({
  data,
  height = 140,
}: {
  data: BarPoint[]
  height?: number
}) {
  const max = Math.max(1, ...data.map((d) => d.value))
  return (
    <div className="flex items-end gap-1.5" style={{ height }}>
      {data.map((d, i) => {
        const pct = (d.value / max) * 100
        return (
          <div key={i} className="group flex min-w-0 flex-1 flex-col items-center justify-end gap-1.5">
            <div className="relative flex w-full flex-1 items-end">
              <div
                className="w-full rounded-t bg-signal/70 transition-all duration-500 group-hover:bg-signal"
                style={{ height: `${Math.max(2, pct)}%` }}
                title={d.hint ?? `${d.label}: ${d.value}`}
              />
            </div>
            <span className="truncate font-mono text-[9px] text-haze/70">{d.label}</span>
          </div>
        )
      })}
    </div>
  )
}
