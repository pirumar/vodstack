import { useEffect, useState } from 'react'
import { KeyRound, Plus, Copy, Check, Trash2, LoaderCircle, ShieldAlert } from 'lucide-react'
import { api, type ApiKey, type ApiKeyCreated } from '../api'
import { Card, PageHeader, EmptyState } from '../components/ui'

export function ApiKeysPage() {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [loading, setLoading] = useState(true)
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [fresh, setFresh] = useState<ApiKeyCreated | null>(null)
  const [copied, setCopied] = useState(false)

  function refresh() {
    api
      .listApiKeys()
      .then(setKeys)
      .catch(() => {})
      .finally(() => setLoading(false))
  }

  useEffect(refresh, [])

  async function create() {
    if (!name.trim()) return
    setCreating(true)
    try {
      const created = await api.createApiKey(name.trim())
      setFresh(created)
      setName('')
      refresh()
    } catch {
      /* ignore */
    } finally {
      setCreating(false)
    }
  }

  async function revoke(k: ApiKey) {
    if (!confirm(`"${k.name || k.id}" anahtarı iptal edilsin mi? Bu geri alınamaz.`)) return
    setKeys((ks) => ks.map((x) => (x.id === k.id ? { ...x, revokedAt: new Date().toISOString() } : x)))
    await api.revokeApiKey(k.id).catch(() => {})
    refresh()
  }

  const fmtDate = (s?: string) => (s ? new Date(s).toLocaleString('tr-TR') : '—')

  return (
    <div>
      <PageHeader
        eyebrow="platform"
        title="API Anahtarları"
        count={keys.length}
        icon={<KeyRound className="h-5 w-5" />}
      />

      {/* Freshly created key — shown once */}
      {fresh && (
        <Card className="mb-6 border-signal/40 bg-signal/5 p-5">
          <div className="mb-2 flex items-center gap-2 text-signal">
            <ShieldAlert className="h-4 w-4" />
            <h3 className="font-display text-sm font-semibold">Anahtarı şimdi kopyala — tekrar gösterilmeyecek</h3>
          </div>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 overflow-x-auto rounded-lg border border-edge bg-ink/70 px-3 py-2 font-mono text-xs text-chalk">
              {fresh.key}
            </code>
            <button
              onClick={() => {
                navigator.clipboard.writeText(fresh.key)
                setCopied(true)
                setTimeout(() => setCopied(false), 1600)
              }}
              className="btn-primary text-xs"
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              Kopyala
            </button>
            <button onClick={() => setFresh(null)} className="btn-ghost text-xs">
              Kapat
            </button>
          </div>
        </Card>
      )}

      {/* Create form */}
      <Card className="mb-6 p-5">
        <label className="eyebrow mb-2 block">yeni anahtar</label>
        <div className="flex flex-wrap items-center gap-2">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && create()}
            placeholder="Anahtar adı (örn. mobil uygulama)"
            className="input min-w-0 flex-1"
          />
          <button onClick={create} disabled={creating || !name.trim()} className="btn-primary text-sm">
            {creating ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
            Oluştur
          </button>
        </div>
      </Card>

      {loading ? (
        <div className="h-40 animate-pulse rounded-2xl border border-edge bg-panel/40" />
      ) : keys.length === 0 ? (
        <EmptyState title="Henüz API anahtarı yok" hint="yukarıdan ilkini oluştur" icon={<KeyRound className="h-8 w-8" />} />
      ) : (
        <Card className="overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-edge bg-ink/40 text-left font-mono text-[10px] uppercase tracking-[0.18em] text-haze">
                <th className="px-4 py-3 font-medium">Ad</th>
                <th className="px-4 py-3 font-medium">Oluşturuldu</th>
                <th className="px-4 py-3 font-medium">Son kullanım</th>
                <th className="px-4 py-3 font-medium">Durum</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => {
                const revoked = !!k.revokedAt
                return (
                  <tr key={k.id} className="border-b border-edge/50 last:border-0">
                    <td className="px-4 py-3 font-medium">{k.name || <span className="text-haze">(adsız)</span>}</td>
                    <td className="px-4 py-3 font-mono text-xs text-haze">{fmtDate(k.createdAt)}</td>
                    <td className="px-4 py-3 font-mono text-xs text-haze">{fmtDate(k.lastUsedAt)}</td>
                    <td className="px-4 py-3">
                      <span
                        className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.18em] ring-1 ${
                          revoked ? 'text-bad ring-bad/40' : 'text-ok ring-ok/40'
                        }`}
                      >
                        <span className={`h-1.5 w-1.5 rounded-full ${revoked ? 'bg-bad' : 'bg-ok'}`} />
                        {revoked ? 'iptal' : 'aktif'}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right">
                      {!revoked && (
                        <button
                          onClick={() => revoke(k)}
                          className="inline-flex items-center justify-center rounded-lg p-2 text-haze transition hover:bg-bad/20 hover:text-bad"
                          aria-label="İptal et"
                          title="İptal et"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  )
}
