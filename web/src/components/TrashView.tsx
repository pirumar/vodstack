import { useCallback, useEffect, useState } from 'react'
import { motion } from 'framer-motion'
import { RotateCcw, Trash2, Clock3 } from 'lucide-react'
import { api, type TrashItem } from '../api'
import { formatBytes, formatDuration } from '../lib/format'

function daysLeft(purgeAt?: string): number | null {
  if (!purgeAt) return null
  const ms = new Date(purgeAt).getTime() - Date.now()
  return Math.max(0, Math.ceil(ms / 86_400_000))
}

export function TrashView() {
  const [items, setItems] = useState<TrashItem[]>([])
  const [retention, setRetention] = useState(15)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const res = await api.listTrash()
      setItems(res.videos)
      setRetention(res.retentionDays)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  async function restore(v: TrashItem) {
    setItems((xs) => xs.filter((x) => x.videoId !== v.videoId))
    await api.restore(v.videoId).catch(refresh)
  }

  async function purge(v: TrashItem) {
    if (!confirm(`"${v.title}" kalıcı olarak silinsin mi? Bu geri alınamaz.`)) return
    setItems((xs) => xs.filter((x) => x.videoId !== v.videoId))
    await api.purge(v.videoId).catch(refresh)
  }

  return (
    <div>
      <div className="mb-4 flex items-center gap-3">
        <Trash2 className="h-4 w-4 text-haze" />
        <h2 className="font-display text-lg font-semibold tracking-tight">Çöp Kutusu</h2>
        <span className="font-mono text-xs text-haze">/ {items.length}</span>
        <span className="ml-2 rounded-full border border-edge bg-panel/50 px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.16em] text-haze">
          {retention} gün sonra otomatik silinir
        </span>
      </div>

      {loading ? (
        <div className="py-16 text-center font-mono text-xs text-haze">yükleniyor…</div>
      ) : items.length === 0 ? (
        <div className="grid place-items-center rounded-2xl border border-dashed border-edge bg-panel/30 py-20 text-center">
          <p className="font-display text-lg font-semibold text-haze">Çöp kutusu boş</p>
          <p className="mt-1 font-mono text-[11px] uppercase tracking-[0.18em] text-haze/60">
            silinen videolar burada {retention} gün bekler
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {items.map((v, i) => {
            const left = daysLeft(v.purgeAt)
            return (
              <motion.div
                key={v.videoId}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: Math.min(i * 0.03, 0.3) }}
                className="flex items-center gap-4 rounded-xl border border-edge bg-panel/50 px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <h3 className="truncate font-display text-sm font-semibold">{v.title}</h3>
                  <div className="mt-1 flex items-center gap-3 font-mono text-[11px] text-haze">
                    <span>{v.availableResolutions || '—'}</span>
                    <span>{formatDuration(v.length)}</span>
                    <span>{formatBytes(v.storageSize)}</span>
                  </div>
                </div>

                <span
                  className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.14em] ring-1 ${
                    left !== null && left <= 3
                      ? 'text-bad ring-bad/30'
                      : 'text-warn ring-warn/30'
                  }`}
                >
                  <Clock3 className="h-3 w-3" />
                  {left !== null ? `${left} gün kaldı` : '—'}
                </span>

                <button
                  onClick={() => restore(v)}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-edge/60 px-3 py-2 text-xs font-semibold transition hover:bg-ok/20 hover:text-ok"
                >
                  <RotateCcw className="h-3.5 w-3.5" /> Geri al
                </button>
                <button
                  onClick={() => purge(v)}
                  className="inline-flex items-center justify-center rounded-lg bg-edge/60 px-3 py-2 text-xs text-haze transition hover:bg-bad/20 hover:text-bad"
                  aria-label="Kalıcı sil"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </motion.div>
            )
          })}
        </div>
      )}
    </div>
  )
}
