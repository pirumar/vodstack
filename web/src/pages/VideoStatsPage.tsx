import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  BarChart3,
  Users,
  Play,
  CheckCircle2,
  Gauge,
  AlertTriangle,
  Clock,
  Hourglass,
  Wifi,
  ArrowLeft,
} from 'lucide-react'
import { api, type AnalyticsRange, type Video, type VideoAnalytics, type ViewerProgressRow } from '../api'
import { useLibrary } from '../LibraryContext'
import { Card, PageHeader, StatTile, EmptyState } from '../components/ui'
import { RangeSelector, CountryPanel } from '../components/analytics'
import { Heatmap } from '../components/charts/Heatmap'
import { formatBytes, formatDuration } from '../lib/format'

// Dedicated per-video statistics screen (Bunny-style): headline views + watch
// time, estimated bandwidth, per-country breakdown, engagement heatmap, and the
// per-viewer progress table. Reached from /videos/:videoId/stats.
export function VideoStatsPage() {
  const { videoId = '' } = useParams()
  const { embedBaseUrl, libraryId } = useLibrary()
  const [video, setVideo] = useState<Video | null>(null)
  const [data, setData] = useState<VideoAnalytics | null>(null)
  const [viewers, setViewers] = useState<ViewerProgressRow[] | null>(null)
  const [heatmap, setHeatmap] = useState<number[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [range, setRange] = useState<AnalyticsRange>('30d')

  useEffect(() => {
    api.getVideo(videoId).then(setVideo).catch(() => {})
    api.getVideoViewers(videoId).then(setViewers).catch(() => {})
  }, [videoId])

  useEffect(() => {
    setLoading(true)
    api
      .getVideoAnalytics(videoId, range)
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [videoId, range])

  useEffect(() => {
    if (!embedBaseUrl) return
    const base = embedBaseUrl.replace(/\/$/, '')
    fetch(`${base}/embed/${libraryId}/${videoId}/heatmap`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => setHeatmap(Array.isArray(d?.heatmap) ? d.heatmap : null))
      .catch(() => {})
  }, [embedBaseUrl, libraryId, videoId])

  const hasData = data && (data.sessions > 0 || data.starts > 0)

  return (
    <div>
      <PageHeader
        eyebrow="video istatistikleri"
        title={video?.title || 'Video'}
        icon={<BarChart3 className="h-5 w-5" />}
        actions={<RangeSelector value={range} onChange={setRange} />}
      />

      <Link
        to={`/library/${videoId}`}
        className="mb-6 inline-flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-[0.14em] text-haze transition hover:text-signal"
      >
        <ArrowLeft className="h-3.5 w-3.5" /> videoya dön
      </Link>

      {loading ? (
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="h-24 animate-pulse rounded-2xl border border-edge bg-panel/40" />
          ))}
        </div>
      ) : !hasData ? (
        <EmptyState
          title="Henüz izleme verisi yok"
          hint="bu video izlendikçe burada toplanır"
          icon={<BarChart3 className="h-8 w-8" />}
        />
      ) : (
        <div className="space-y-8">
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
            <StatTile label="toplam izlenme" value={data!.starts} tone="signal" icon={<Play className="h-4 w-4" />} />
            <StatTile label="tekil izleyici" value={data!.sessions} icon={<Users className="h-4 w-4" />} />
            <StatTile
              label="toplam izlenme süresi"
              value={formatDuration(data!.totalWatchSeconds)}
              icon={<Clock className="h-4 w-4" />}
            />
            <StatTile
              label="ort. izlenme süresi"
              value={formatDuration(data!.avgWatchSeconds)}
              icon={<Hourglass className="h-4 w-4" />}
            />
            <StatTile
              label="bandwidth (tahmini)"
              value={formatBytes(data!.estBandwidthBytes)}
              icon={<Wifi className="h-4 w-4" />}
            />
            <StatTile label="tamamlanma" value={data!.completions} tone="ok" icon={<CheckCircle2 className="h-4 w-4" />} />
          </div>

          <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
            <StatTile
              label="ort. başlangıç"
              value={`${Math.round(data!.avgStartupMs)}ms`}
              icon={<Gauge className="h-4 w-4" />}
            />
            <StatTile label="rebuffer" value={data!.rebuffers} tone={data!.rebuffers ? 'warn' : undefined} />
            <StatTile
              label="hata"
              value={data!.errors}
              tone={data!.errors ? 'bad' : undefined}
              icon={<AlertTriangle className="h-4 w-4" />}
            />
          </div>

          {heatmap && heatmap.length > 0 && (
            <Card className="p-5">
              <div className="eyebrow mb-2">izlenme yoğunluğu</div>
              <Heatmap buckets={heatmap} duration={video?.length} />
            </Card>
          )}

          <CountryPanel rows={data!.byCountry} />

          <Card className="p-5">
            <div className="mb-3 flex items-center gap-2">
              <Users className="h-4 w-4 text-signal" />
              <h4 className="font-display text-sm font-semibold">Bu videoyu izleyenler</h4>
            </div>
            {viewers && viewers.length > 0 ? (
              <div className="overflow-hidden rounded-xl border border-edge">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-edge bg-ink/40 text-left font-mono text-[10px] uppercase tracking-[0.18em] text-haze">
                      <th className="px-4 py-2.5 font-medium">İzleyici</th>
                      <th className="px-4 py-2.5 text-right font-medium">İlerleme</th>
                      <th className="px-4 py-2.5 text-right font-medium">Konum</th>
                      <th className="px-4 py-2.5 text-right font-medium">Durum</th>
                      <th className="px-4 py-2.5 text-right font-medium">Son izleme</th>
                    </tr>
                  </thead>
                  <tbody>
                    {viewers.map((vw) => (
                      <tr key={vw.viewerId} className="border-b border-edge/50 transition last:border-0 hover:bg-edge/20">
                        <td className="px-4 py-2.5 font-mono text-chalk">{vw.viewerId}</td>
                        <td className="px-4 py-2.5 text-right font-mono text-signal">{Math.round(vw.watchedPercent)}%</td>
                        <td className="px-4 py-2.5 text-right font-mono text-haze">{formatDuration(Math.round(vw.position))}</td>
                        <td className="px-4 py-2.5 text-right font-mono">
                          {vw.completed ? <span className="text-ok">tamamlandı</span> : <span className="text-warn">devam ediyor</span>}
                        </td>
                        <td className="px-4 py-2.5 text-right font-mono text-haze">
                          {new Date(vw.lastWatchedAt).toLocaleDateString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="font-mono text-[11px] text-haze">Henüz izleyen yok.</p>
            )}
          </Card>
        </div>
      )}
    </div>
  )
}
