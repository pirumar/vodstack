import { STATUS_LABEL, type VideoStatus } from '../api'

const STYLES: Record<VideoStatus, { dot: string; text: string; ring: string }> = {
  0: { dot: 'bg-idle', text: 'text-haze', ring: 'ring-idle/30' },
  1: { dot: 'bg-warn', text: 'text-warn', ring: 'ring-warn/30' },
  2: { dot: 'bg-warn animate-pulse-rec', text: 'text-warn', ring: 'ring-warn/30' },
  3: { dot: 'bg-signal animate-pulse-rec', text: 'text-signal', ring: 'ring-signal/30' },
  4: { dot: 'bg-ok', text: 'text-ok', ring: 'ring-ok/30' },
  5: { dot: 'bg-bad', text: 'text-bad', ring: 'ring-bad/30' },
}

export function StatusChip({ status }: { status: VideoStatus }) {
  const s = STYLES[status]
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full bg-ink/60 px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.18em] ring-1 ${s.ring} ${s.text}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${s.dot}`} />
      {STATUS_LABEL[status]}
    </span>
  )
}
