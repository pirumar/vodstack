import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  BarChart3,
  Users,
  Play,
  CheckCircle2,
  Gauge,
  AlertTriangle,
  TrendingUp,
  Clock,
  Hourglass,
  Wifi,
} from 'lucide-react'
import { api, type AnalyticsRange, type LibraryAnalytics } from '../api'
import { Card, PageHeader, StatTile, EmptyState } from '../components/ui'
import { MiniBarChart } from '../components/charts/MiniBarChart'
import { RangeSelector, CountryPanel } from '../components/analytics'
import { formatBytes, formatDuration } from '../lib/format'

export function AnalyticsPage() {
  const [data, setData] = useState<LibraryAnalytics | null>(null)
  const [loading, setLoading] = useState(true)
  const [range, setRange] = useState<AnalyticsRange>('30d')

  useEffect(() => {
    setLoading(true)
    api
      .getLibraryAnalytics(range)
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [range])

  const hasData = data && (data.sessions > 0 || data.starts > 0)

  return (
    <div>
      <PageHeader
        eyebrow="kütüphane geneli"
        title="Analitik"
        icon={<BarChart3 className="h-5 w-5" />}
        actions={<RangeSelector value={range} onChange={setRange} />}
      />

      {loading ? (
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="h-24 animate-pulse rounded-2xl border border-edge bg-panel/40" />
          ))}
        </div>
      ) : !hasData ? (
        <EmptyState
          title="Henüz izleme verisi yok"
          hint="videolar izlendikçe burada toplanır"
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

          {/* QoE secondary row */}
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

          {/* View history */}
          <Card className="p-5">
            <div className="mb-4 flex items-center gap-2">
              <TrendingUp className="h-4 w-4 text-signal" />
              <h3 className="font-display text-sm font-semibold">İzlenme geçmişi</h3>
            </div>
            {data!.daily.length > 0 ? (
              <MiniBarChart
                data={data!.daily.map((d) => ({
                  label: d.date.slice(5),
                  value: d.starts,
                  hint: `${d.date}: ${d.starts} başlatma · ${d.sessions} oturum`,
                }))}
              />
            ) : (
              <p className="font-mono text-[11px] text-haze">Veri yok.</p>
            )}
          </Card>

          <CountryPanel rows={data!.byCountry} />

          {/* Top videos */}
          <Card className="p-5">
            <div className="mb-4 flex items-center gap-2">
              <Play className="h-4 w-4 text-signal" />
              <h3 className="font-display text-sm font-semibold">En çok izlenen videolar</h3>
            </div>
            {data!.topVideos.length > 0 ? (
              <div className="overflow-hidden rounded-xl border border-edge">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-edge bg-ink/40 text-left font-mono text-[10px] uppercase tracking-[0.18em] text-haze">
                      <th className="px-4 py-2.5 font-medium">#</th>
                      <th className="px-4 py-2.5 font-medium">Video</th>
                      <th className="px-4 py-2.5 text-right font-medium">Başlatma</th>
                      <th className="px-4 py-2.5 text-right font-medium">Oturum</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data!.topVideos.map((t, i) => (
                      <tr key={t.videoId} className="border-b border-edge/50 transition last:border-0 hover:bg-edge/20">
                        <td className="px-4 py-2.5 font-mono text-haze">{i + 1}</td>
                        <td className="px-4 py-2.5">
                          <Link to={`/videos/${t.videoId}/stats`} className="text-chalk transition hover:text-signal">
                            {t.title || t.videoId}
                          </Link>
                        </td>
                        <td className="px-4 py-2.5 text-right font-mono text-signal">{t.starts}</td>
                        <td className="px-4 py-2.5 text-right font-mono">{t.sessions}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="font-mono text-[11px] text-haze">Veri yok.</p>
            )}
          </Card>
        </div>
      )}
    </div>
  )
}
