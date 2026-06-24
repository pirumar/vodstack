import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  LayoutDashboard,
  Film,
  Radio,
  Loader,
  Clock,
  Users,
  Play,
  Upload,
  KeyRound,
  ArrowRight,
} from 'lucide-react'
import { api, type Video, type LibraryAnalytics } from '../api'
import { Card, PageHeader, StatTile, EmptyState } from '../components/ui'
import { StatusChip } from '../components/StatusChip'
import { formatDuration } from '../lib/format'

export function OverviewPage() {
  const [videos, setVideos] = useState<Video[]>([])
  const [analytics, setAnalytics] = useState<LibraryAnalytics | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([api.listVideos(), api.getLibraryAnalytics().catch(() => null)])
      .then(([vs, a]) => {
        setVideos(vs)
        setAnalytics(a)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const ready = videos.filter((v) => v.status === 4).length
  const working = videos.filter((v) => v.status >= 1 && v.status <= 3).length
  const totalSecs = videos.reduce((a, v) => a + (v.length || 0), 0)
  const recent = videos.slice(0, 6)

  return (
    <div>
      <PageHeader eyebrow="panel" title="Genel Bakış" icon={<LayoutDashboard className="h-5 w-5" />} />

      {loading ? (
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="h-24 animate-pulse rounded-2xl border border-edge bg-panel/40" />
          ))}
        </div>
      ) : (
        <div className="space-y-8">
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <StatTile label="varlık" value={videos.length} icon={<Film className="h-4 w-4" />} />
            <StatTile label="yayında" value={ready} tone="ok" icon={<Radio className="h-4 w-4" />} />
            <StatTile label="işleniyor" value={working} tone="signal" icon={<Loader className="h-4 w-4" />} />
            <StatTile label="toplam saat" value={(totalSecs / 3600).toFixed(1)} icon={<Clock className="h-4 w-4" />} />
          </div>

          {analytics && (analytics.sessions > 0 || analytics.starts > 0) && (
            <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
              <StatTile label="oturum" value={analytics.sessions} icon={<Users className="h-4 w-4" />} />
              <StatTile label="başlatma" value={analytics.starts} tone="signal" icon={<Play className="h-4 w-4" />} />
              <StatTile label="tamamlanma" value={analytics.completions} tone="ok" />
              <Link to="/analytics" className="block">
                <Card className="flex h-full items-center justify-between p-4 transition hover:border-signal/40">
                  <span className="font-display text-sm font-semibold">Tüm analitik</span>
                  <ArrowRight className="h-4 w-4 text-signal" />
                </Card>
              </Link>
            </div>
          )}

          {/* Quick actions */}
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <QuickAction to="/library" icon={<Upload className="h-4 w-4" />} title="Video yükle" hint="kütüphaneye yeni içerik" />
            <QuickAction to="/api-keys" icon={<KeyRound className="h-4 w-4" />} title="API anahtarı" hint="entegrasyon için anahtar oluştur" />
            <QuickAction to="/webhooks" icon={<Radio className="h-4 w-4" />} title="Webhook ekle" hint="event bildirimleri" />
          </div>

          {/* Recent videos */}
          <div>
            <div className="mb-4 flex items-center justify-between">
              <h3 className="font-display text-sm font-semibold">Son videolar</h3>
              <Link to="/library" className="inline-flex items-center gap-1 font-mono text-xs text-haze transition hover:text-signal">
                Tümü <ArrowRight className="h-3 w-3" />
              </Link>
            </div>
            {recent.length === 0 ? (
              <EmptyState title="Henüz video yok" hint="kütüphaneden ilk videonu yükle" icon={<Film className="h-8 w-8" />} />
            ) : (
              <div className="space-y-2">
                {recent.map((v) => (
                  <Link
                    key={v.videoId}
                    to={`/library/${v.videoId}`}
                    className="flex items-center gap-4 rounded-xl border border-edge bg-panel/60 px-4 py-3 transition hover:border-signal/40"
                  >
                    <Film className="h-4 w-4 shrink-0 text-haze" />
                    <span className="min-w-0 flex-1 truncate font-medium">{v.title}</span>
                    <span className="font-mono text-xs text-haze">{formatDuration(v.length)}</span>
                    <StatusChip status={v.status} />
                  </Link>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function QuickAction({
  to,
  icon,
  title,
  hint,
}: {
  to: string
  icon: ReactNode
  title: string
  hint: string
}) {
  return (
    <Link
      to={to}
      className="group flex items-center gap-3 rounded-xl border border-edge bg-panel/60 p-4 transition hover:border-signal/40"
    >
      <span className="grid h-10 w-10 place-items-center rounded-lg bg-signal/15 text-signal ring-1 ring-signal/30">
        {icon}
      </span>
      <span className="min-w-0">
        <span className="block font-display text-sm font-semibold">{title}</span>
        <span className="block truncate font-mono text-[10px] text-haze">{hint}</span>
      </span>
      <ArrowRight className="ml-auto h-4 w-4 text-haze transition group-hover:translate-x-0.5 group-hover:text-signal" />
    </Link>
  )
}
