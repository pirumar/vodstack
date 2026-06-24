import { useEffect, useState } from 'react'
import { Webhook, Plus, Copy, Check, Trash2, LoaderCircle, ShieldAlert } from 'lucide-react'
import { api, type WebhookEndpoint, type WebhookCreated, WEBHOOK_EVENTS } from '../api'
import { Card, PageHeader, EmptyState } from '../components/ui'

export function WebhooksPage() {
  const [hooks, setHooks] = useState<WebhookEndpoint[]>([])
  const [loading, setLoading] = useState(true)
  const [url, setUrl] = useState('')
  const [events, setEvents] = useState<string[]>([])
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const [fresh, setFresh] = useState<WebhookCreated | null>(null)
  const [copied, setCopied] = useState(false)

  function refresh() {
    api
      .listWebhooks()
      .then(setHooks)
      .catch(() => {})
      .finally(() => setLoading(false))
  }

  useEffect(refresh, [])

  function toggleEvent(ev: string) {
    setEvents((es) => (es.includes(ev) ? es.filter((e) => e !== ev) : [...es, ev]))
  }

  async function create() {
    setError('')
    if (!/^https?:\/\/.+/.test(url.trim())) {
      setError('Geçerli bir URL girin (https://...)')
      return
    }
    setCreating(true)
    try {
      const created = await api.createWebhook(url.trim(), events)
      setFresh(created)
      setUrl('')
      setEvents([])
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  async function remove(h: WebhookEndpoint) {
    if (!confirm(`${h.url} endpoint'i silinsin mi?`)) return
    setHooks((hs) => hs.filter((x) => x.id !== h.id))
    await api.deleteWebhook(h.id).catch(() => {})
    refresh()
  }

  const fmtDate = (s?: string) => (s ? new Date(s).toLocaleString('tr-TR') : '—')

  return (
    <div>
      <PageHeader eyebrow="platform" title="Webhook'lar" count={hooks.length} icon={<Webhook className="h-5 w-5" />} />

      {fresh && (
        <Card className="mb-6 border-signal/40 bg-signal/5 p-5">
          <div className="mb-2 flex items-center gap-2 text-signal">
            <ShieldAlert className="h-4 w-4" />
            <h3 className="font-display text-sm font-semibold">İmzalama secret'ını şimdi kopyala — tekrar gösterilmeyecek</h3>
          </div>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 overflow-x-auto rounded-lg border border-edge bg-ink/70 px-3 py-2 font-mono text-xs text-chalk">
              {fresh.secret}
            </code>
            <button
              onClick={() => {
                navigator.clipboard.writeText(fresh.secret)
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
        <label className="eyebrow mb-2 block">yeni endpoint</label>
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://example.com/webhooks/vodstack"
          className="input mb-3 w-full"
        />
        <div className="mb-3 flex flex-wrap gap-2">
          {WEBHOOK_EVENTS.map((ev) => {
            const on = events.includes(ev)
            return (
              <button
                key={ev}
                onClick={() => toggleEvent(ev)}
                className={`rounded-lg border px-3 py-1.5 font-mono text-xs transition ${
                  on ? 'border-signal/50 bg-signal/15 text-signal' : 'border-edge bg-ink/40 text-haze hover:text-chalk'
                }`}
              >
                {ev}
              </button>
            )
          })}
          <span className="self-center font-mono text-[10px] text-haze/60">
            (hiçbiri seçilmezse tüm event'ler)
          </span>
        </div>
        {error && <p className="mb-3 font-mono text-xs text-bad">{error}</p>}
        <button onClick={create} disabled={creating} className="btn-primary text-sm">
          {creating ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
          Endpoint ekle
        </button>
      </Card>

      {loading ? (
        <div className="h-40 animate-pulse rounded-2xl border border-edge bg-panel/40" />
      ) : hooks.length === 0 ? (
        <EmptyState title="Henüz webhook yok" hint="event bildirimleri için bir endpoint ekle" icon={<Webhook className="h-8 w-8" />} />
      ) : (
        <div className="space-y-3">
          {hooks.map((h) => (
            <Card key={h.id} className="flex flex-wrap items-center gap-4 p-4">
              <div className="min-w-0 flex-1">
                <div className="truncate font-mono text-sm text-chalk">{h.url}</div>
                <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                  {(h.events.length ? h.events : ['tüm event\'ler']).map((e) => (
                    <span key={e} className="rounded bg-edge/60 px-2 py-0.5 font-mono text-[10px] text-haze">
                      {e}
                    </span>
                  ))}
                </div>
              </div>
              <div className="font-mono text-[10px] text-haze/70">{fmtDate(h.createdAt)}</div>
              <span
                className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.18em] ring-1 ${
                  h.active ? 'text-ok ring-ok/40' : 'text-idle ring-edge'
                }`}
              >
                <span className={`h-1.5 w-1.5 rounded-full ${h.active ? 'bg-ok' : 'bg-idle'}`} />
                {h.active ? 'aktif' : 'pasif'}
              </span>
              <button
                onClick={() => remove(h)}
                className="inline-flex items-center justify-center rounded-lg p-2 text-haze transition hover:bg-bad/20 hover:text-bad"
                aria-label="Sil"
                title="Sil"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
