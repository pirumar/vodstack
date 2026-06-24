import { useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Search, Settings2, LoaderCircle, Play, Film } from 'lucide-react'
import { api, type SearchHit } from '../api'
import { PageHeader, Card, EmptyState } from '../components/ui'
import { SearchSettings } from '../components/SearchSettings'
import { LlmSettings } from '../components/LlmSettings'
import { formatDuration } from '../lib/format'

// Library-wide in-video search. Type a query, get transcript moments grouped by
// video; click a timestamp to open the video at that second.
export function SearchPage() {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  const [enabled, setEnabled] = useState(true)
  const [busy, setBusy] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const timer = useRef<number | null>(null)

  function onChange(v: string) {
    setQ(v)
    if (timer.current) window.clearTimeout(timer.current)
    if (!v.trim()) {
      setHits(null)
      return
    }
    timer.current = window.setTimeout(() => run(v), 350)
  }

  async function run(query: string) {
    setBusy(true)
    try {
      const r = await api.search(query)
      setEnabled(r.enabled)
      setHits(r.results)
    } catch {
      setHits([])
    } finally {
      setBusy(false)
    }
  }

  // Group consecutive hits by video, preserving fused-score order.
  const groups = useMemo(() => {
    if (!hits) return []
    const byVideo = new Map<string, { title: string; hits: SearchHit[] }>()
    for (const h of hits) {
      const g = byVideo.get(h.videoId) ?? { title: h.title, hits: [] }
      g.hits.push(h)
      byVideo.set(h.videoId, g)
    }
    return Array.from(byVideo.entries()).map(([videoId, g]) => ({ videoId, ...g }))
  }, [hits])

  const openAt = (videoId: string, sec: number) =>
    navigate(`/library/${videoId}?t=${Math.floor(sec)}`)

  return (
    <div>
      <PageHeader
        eyebrow="içerik zekası"
        title="Video içinde ara"
        icon={<Search className="h-5 w-5" />}
        actions={
          <button onClick={() => setShowSettings((v) => !v)} className="btn-ghost text-xs">
            <Settings2 className="h-3.5 w-3.5" /> Ayarlar
          </button>
        }
      />

      {showSettings && (
        <div className="mb-7 grid max-w-3xl gap-4 md:grid-cols-2">
          <SearchSettings />
          <LlmSettings />
        </div>
      )}

      {/* Search box */}
      <div className="relative max-w-2xl">
        <Search className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-haze" />
        <input
          value={q}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Transkriptlerde anlamsal ara… (ör. 'pisagor teoremi')"
          className="input w-full py-3 pl-11 pr-10 text-base"
          autoFocus
        />
        {busy && (
          <LoaderCircle className="absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 animate-spin text-signal" />
        )}
      </div>

      {/* Results */}
      <div className="mt-7">
        {!enabled ? (
          <EmptyState
            title="Arama etkin değil"
            hint="yukarıdaki Ayarlar'dan video içinde aramayı aç"
            icon={<Search className="h-7 w-7" />}
          />
        ) : hits === null ? (
          <EmptyState
            title="Bir şeyler ara"
            hint="anlamına göre eşleşir — birebir kelime gerekmez"
            icon={<Search className="h-7 w-7" />}
          />
        ) : groups.length === 0 ? (
          <EmptyState title="Eşleşme yok" hint="farklı bir ifade dene veya videoları indeksle" />
        ) : (
          <div className="space-y-4">
            {groups.map((g) => (
              <Card key={g.videoId} className="p-4">
                <div className="mb-3 flex items-center gap-2">
                  <Film className="h-4 w-4 shrink-0 text-signal" />
                  <button
                    onClick={() => openAt(g.videoId, g.hits[0].startSec)}
                    className="truncate font-display text-sm font-semibold transition hover:text-signal"
                    title={g.title}
                  >
                    {g.title}
                  </button>
                </div>
                <div className="space-y-2">
                  {g.hits.map((h, i) => (
                    <button
                      key={i}
                      onClick={() => openAt(h.videoId, h.startSec)}
                      className="group flex w-full items-start gap-3 rounded-lg border border-edge bg-ink/40 px-3 py-2.5 text-left transition hover:border-signal/40"
                    >
                      <span className="mt-0.5 inline-flex shrink-0 items-center gap-1 rounded-md bg-signal/15 px-2 py-1 font-mono text-[11px] text-signal ring-1 ring-signal/30">
                        <Play className="h-3 w-3" />
                        {formatDuration(Math.round(h.startSec))}
                      </span>
                      <span className="min-w-0 flex-1 text-sm leading-snug text-haze transition group-hover:text-chalk">
                        {h.snippet}
                      </span>
                    </button>
                  ))}
                </div>
              </Card>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
