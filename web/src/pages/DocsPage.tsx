import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  BookOpen,
  KeyRound,
  Webhook,
  ShieldCheck,
  Workflow,
  Plug,
  Copy,
  Check,
  Info,
  AlertTriangle,
  Terminal,
} from 'lucide-react'
import { WEBHOOK_EVENTS } from '../api'

// Salt-okunur entegrasyon kılavuzu. Canlı veri çekmez; hâlihazırda çalışan
// API key + webhook davranışını entegratörler için belgeler. Event listesi tek
// kaynaktan (api.ts → WEBHOOK_EVENTS) beslenir.

// ---------------------------------------------------------------------------
// İçindekiler navigasyonu
// ---------------------------------------------------------------------------

const SECTIONS = [
  { id: 'baslangic', label: 'Hızlı başlangıç', icon: Workflow },
  { id: 'kimlik', label: 'Kimlik doğrulama', icon: KeyRound },
  { id: 'uc-noktalar', label: 'Uç noktalar', icon: Plug },
  { id: 'webhooks', label: "Webhook'lar", icon: Webhook },
  { id: 'imza', label: 'İmza doğrulama', icon: ShieldCheck },
] as const

function useActiveSection(ids: string[]) {
  const [active, setActive] = useState(ids[0])
  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting)
        if (visible.length) {
          const top = visible.reduce((a, b) =>
            a.boundingClientRect.top < b.boundingClientRect.top ? a : b,
          )
          setActive(top.target.id)
        }
      },
      { rootMargin: '-15% 0px -75% 0px', threshold: 0 },
    )
    ids.forEach((id) => {
      const el = document.getElementById(id)
      if (el) observer.observe(el)
    })
    return () => observer.disconnect()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids.join(',')])
  return active
}

function Toc() {
  const active = useActiveSection(SECTIONS.map((s) => s.id))
  return (
    <aside className="hidden xl:block">
      <div className="sticky top-8">
        <div className="eyebrow mb-3 pl-3">bu sayfada</div>
        <nav className="space-y-0.5 border-l border-edge">
          {SECTIONS.map((s) => {
            const on = active === s.id
            return (
              <a
                key={s.id}
                href={`#${s.id}`}
                onClick={(e) => {
                  e.preventDefault()
                  document.getElementById(s.id)?.scrollIntoView({ behavior: 'smooth' })
                }}
                className={`-ml-px flex items-center gap-2 border-l-2 py-1.5 pl-3 text-sm transition ${
                  on
                    ? 'border-signal text-signal'
                    : 'border-transparent text-haze hover:text-chalk'
                }`}
              >
                <s.icon className="h-3.5 w-3.5" />
                {s.label}
              </a>
            )
          })}
        </nav>
      </div>
    </aside>
  )
}

// ---------------------------------------------------------------------------
// Yapı taşları
// ---------------------------------------------------------------------------

function Section({
  id,
  icon,
  title,
  kicker,
  children,
}: {
  id: string
  icon: ReactNode
  title: string
  kicker?: string
  children: ReactNode
}) {
  return (
    <section id={id} className="scroll-mt-24">
      <div className="mb-4 flex items-center gap-3">
        <span className="grid h-9 w-9 place-items-center rounded-lg bg-signal/10 text-signal ring-1 ring-signal/25">
          {icon}
        </span>
        <div>
          {kicker && <div className="eyebrow mb-0.5">{kicker}</div>}
          <h2 className="font-display text-xl font-semibold tracking-tight text-chalk">
            {title}
          </h2>
        </div>
      </div>
      <div className="space-y-4">{children}</div>
    </section>
  )
}

function Panel({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <div className={`rounded-2xl border border-edge bg-panel/60 shadow-deck ${className}`}>
      {children}
    </div>
  )
}

function Inline({ children }: { children: ReactNode }) {
  return (
    <code className="rounded bg-edge/60 px-1.5 py-0.5 font-mono text-[11px] text-chalk">
      {children}
    </code>
  )
}

function Callout({
  tone = 'info',
  title,
  children,
}: {
  tone?: 'info' | 'warn'
  title: string
  children: ReactNode
}) {
  const warn = tone === 'warn'
  return (
    <div
      className={`flex gap-3 rounded-xl border p-4 ${
        warn ? 'border-warn/30 bg-warn/5' : 'border-signal/25 bg-signal/5'
      }`}
    >
      <span className={warn ? 'text-warn' : 'text-signal'}>
        {warn ? <AlertTriangle className="h-4 w-4" /> : <Info className="h-4 w-4" />}
      </span>
      <div className="min-w-0 text-sm leading-relaxed">
        <p className={`mb-0.5 font-semibold ${warn ? 'text-warn' : 'text-signal'}`}>{title}</p>
        <div className="text-haze">{children}</div>
      </div>
    </div>
  )
}

const METHOD_TONE: Record<string, string> = {
  GET: 'text-ok ring-ok/30 bg-ok/10',
  POST: 'text-signal ring-signal/30 bg-signal/10',
  PUT: 'text-warn ring-warn/30 bg-warn/10',
  DELETE: 'text-bad ring-bad/30 bg-bad/10',
}

function Method({ m }: { m: string }) {
  return (
    <span
      className={`inline-block w-16 rounded-md px-2 py-0.5 text-center font-mono text-[10px] font-semibold uppercase tracking-wider ring-1 ${
        METHOD_TONE[m] ?? 'text-haze ring-edge bg-edge/40'
      }`}
    >
      {m}
    </span>
  )
}

// Başlık çubuklu, kopyalanabilir kod bloğu.
function CodeBlock({ code, lang = 'bash' }: { code: string; lang?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="overflow-hidden rounded-xl border border-edge bg-ink/80">
      <div className="flex items-center justify-between border-b border-edge/70 bg-graphite/60 px-3 py-1.5">
        <span className="flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-[0.18em] text-haze">
          <Terminal className="h-3 w-3" />
          {lang}
        </span>
        <button
          onClick={() => {
            navigator.clipboard.writeText(code)
            setCopied(true)
            setTimeout(() => setCopied(false), 1600)
          }}
          className="inline-flex items-center gap-1 rounded-md px-2 py-0.5 font-mono text-[10px] text-haze transition hover:text-chalk"
          aria-label="Kopyala"
        >
          {copied ? <Check className="h-3 w-3 text-ok" /> : <Copy className="h-3 w-3" />}
          {copied ? 'kopyalandı' : 'kopyala'}
        </button>
      </div>
      <pre className="overflow-x-auto p-4 font-mono text-xs leading-relaxed text-chalk">
        <code>{code}</code>
      </pre>
    </div>
  )
}

// Çok-dilli sekmeli kod örneği.
function CodeTabs({ tabs }: { tabs: { label: string; lang: string; code: string }[] }) {
  const [i, setI] = useState(0)
  return (
    <div>
      <div className="mb-2 flex gap-1">
        {tabs.map((t, idx) => (
          <button
            key={t.label}
            onClick={() => setI(idx)}
            className={`rounded-lg px-3 py-1.5 font-mono text-xs transition ${
              i === idx
                ? 'bg-signal/15 text-signal ring-1 ring-signal/30'
                : 'text-haze hover:text-chalk'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      <CodeBlock code={tabs[i].code} lang={tabs[i].lang} />
    </div>
  )
}

function Table({ head, children }: { head: string[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-xl border border-edge">
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-edge bg-ink/40 font-mono text-[10px] uppercase tracking-wider text-haze">
            {head.map((h) => (
              <th key={h} className="px-3 py-2 font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Veri
// ---------------------------------------------------------------------------

const ENDPOINTS: { method: string; path: string; desc: string }[] = [
  { method: 'POST', path: '/videos', desc: 'Yeni video kaydı oluşturur (status 0).' },
  { method: 'GET', path: '/videos/{id}', desc: 'Video meta verisi + işleme durumu.' },
  { method: 'DELETE', path: '/videos/{id}', desc: 'Videoyu çöpe taşır (soft-delete).' },
  { method: 'POST', path: '/videos/{id}/upload-url', desc: 'Ham kaynak için presigned PUT URL üretir.' },
  { method: 'POST', path: '/videos/{id}/complete', desc: 'Yüklemeyi onaylar, transcode kuyruğa alır.' },
  { method: 'POST', path: '/videos/{id}/fetch', desc: 'Bir URL’den videoyu içe aktarır (Bunny/Vimeo/MP4).' },
  { method: 'GET', path: '/videos/{id}/play', desc: 'İmzalı HLS + poster/altyazı URL’leri döner.' },
  { method: 'GET', path: '/videos/{id}/download', desc: 'Orijinal dosya için imzalı indirme URL’i.' },
  { method: 'POST', path: '/viewer-token', desc: 'İzleyici token’ı üretir (ilerleme/analitik için).' },
  { method: 'GET', path: '/search', desc: 'Video-içi semantik/sözcüksel arama.' },
]

const ERRORS: { code: string; meaning: string }[] = [
  { code: '400', meaning: 'Geçersiz istek / eksik alan.' },
  { code: '401', meaning: 'API anahtarı eksik veya geçersiz.' },
  { code: '403', meaning: 'İptal edilmiş anahtar veya erişim politikası reddi.' },
  { code: '404', meaning: 'Kaynak bulunamadı.' },
  { code: '429', meaning: 'Rate limit aşıldı (Retry-After başlığına bak).' },
  { code: '500', meaning: 'Sunucu hatası.' },
]

const EVENT_DOCS: Record<string, { when: string; data: string }> = {
  'video.created': { when: 'Video kaydı oluşturulduğunda.', data: 'title, status' },
  'video.uploaded': { when: 'Kaynak yüklendi/içe aktarıldı, transcode kuyruğa alındı.', data: 'status, source?' },
  'video.encoded': { when: 'H.264 ABR HLS kodlaması bittiğinde.', data: 'status, durationSeconds, availableResolutions' },
  'video.av1_ready': { when: 'Ek codec’ler (AV1/HEVC/VP9) hazır olduğunda.', data: 'codecs' },
  'video.captioned': { when: 'Otomatik altyazı üretildiğinde.', data: 'lang, auto' },
  'video.indexed': { when: 'Video-içi arama indeksi tamamlandığında.', data: 'lang, chunks, provider' },
  'video.enriched': { when: 'AI içerik üretimi (özet/etiket/bölüm) bittiğinde.', data: 'kinds' },
  'video.encrypted': { when: 'AES-128 şifreleme tamamlandığında.', data: 'encryptionMode' },
  'video.failed': { when: 'Transcode başarısız olduğunda.', data: 'status, error' },
  'video.deleted': { when: 'Video silindiğinde/temizlendiğinde.', data: '— (alan yok)' },
}

const STATUS_STEPS = [
  { n: 0, label: 'created' },
  { n: 1, label: 'uploaded' },
  { n: 2, label: 'processing' },
  { n: 3, label: 'transcoding' },
  { n: 4, label: 'finished' },
  { n: 5, label: 'failed' },
]

// ---------------------------------------------------------------------------
// Kod örnekleri
// ---------------------------------------------------------------------------

const FLOW_EXAMPLE = `KEY="Bearer vds_..."           # API Anahtarları sayfasından
API=https://stream.ornek.com/api/library/default

# 1. Video kaydı oluştur
VID=$(curl -s -X POST $API/videos -H "Authorization: $KEY" \\
      -H 'Content-Type: application/json' -d '{"title":"Ders 1"}' | jq -r .videoId)

# 2. Presigned PUT URL al ve kaynağı yükle
URL=$(curl -s -X POST $API/videos/$VID/upload-url -H "Authorization: $KEY" | jq -r .url)
curl -X PUT --upload-file ders.mp4 "$URL"

# 3. Yüklemeyi onayla -> transcode kuyruğa girer
curl -s -X POST $API/videos/$VID/complete -H "Authorization: $KEY"

# 4. Durumu yokla (status == 4 -> bitti)
curl -s $API/videos/$VID -H "Authorization: $KEY" | jq '{status, availableResolutions}'

# 5. İmzalı HLS URL'ini al ve oynat
curl -s $API/videos/$VID/play -H "Authorization: $KEY" | jq '{hlsUrl, posterUrl}'`

const AUTH_TABS = [
  {
    label: 'cURL',
    lang: 'bash',
    code: `# Önerilen başlık
curl https://stream.ornek.com/api/library/default/videos/$VID \\
  -H "Authorization: Bearer vds_xxx"

# Bunny uyumluluk fallback'i (aynı anahtar)
curl https://stream.ornek.com/api/library/default/videos/$VID \\
  -H "AccessKey: vds_xxx"`,
  },
  {
    label: 'Node.js',
    lang: 'javascript',
    code: `const res = await fetch(
  "https://stream.ornek.com/api/library/default/videos/" + videoId,
  { headers: { Authorization: "Bearer " + process.env.VODSTACK_KEY } },
)
if (res.status === 429) {
  const retry = Number(res.headers.get("Retry-After") ?? 1)
  // retry saniye sonra tekrar dene
}
const video = await res.json()`,
  },
  {
    label: 'Python',
    lang: 'python',
    code: `import requests

r = requests.get(
    f"https://stream.ornek.com/api/library/default/videos/{video_id}",
    headers={"Authorization": f"Bearer {VODSTACK_KEY}"},
)
r.raise_for_status()
video = r.json()`,
  },
]

const WEBHOOK_PAYLOAD = `POST https://example.com/webhooks/vodstack
Content-Type: application/json
User-Agent: vodstack-webhooks/1
X-Vodstack-Event: video.encoded
X-Vodstack-Delivery: 9f1c2e54-...
X-Vodstack-Signature: sha256=8a1b...

{
  "event": "video.encoded",
  "libraryId": "default",
  "videoId": "8c0e...-uuid",
  "timestamp": "2026-06-14T12:34:56Z",
  "data": {
    "status": 4,
    "durationSeconds": 612.5,
    "availableResolutions": ["360p", "720p", "1080p"]
  }
}`

const VERIFY_TABS = [
  {
    label: 'Node.js',
    lang: 'javascript',
    code: `import crypto from 'node:crypto'
import express from 'express'

// secret: endpoint oluşturulurken bir kez gösterilen "whsec_..." değeri
function verify(rawBody, signatureHeader, secret) {
  const expected =
    'sha256=' + crypto.createHmac('sha256', secret).update(rawBody).digest('hex')
  const a = Buffer.from(expected)
  const b = Buffer.from(signatureHeader || '')
  return a.length === b.length && crypto.timingSafeEqual(a, b)
}

// imzayı HAM gövde üzerinden hesapla (JSON.parse'tan ÖNCE)
app.post('/webhooks/vodstack', express.raw({ type: 'application/json' }), (req, res) => {
  if (!verify(req.body, req.get('X-Vodstack-Signature'), process.env.VODSTACK_WHSEC)) {
    return res.sendStatus(401)
  }
  const event = JSON.parse(req.body.toString())
  // ... event.event'e göre işle, hızlıca 2xx dön (15 sn timeout) ...
  res.sendStatus(200)
})`,
  },
  {
    label: 'Python',
    lang: 'python',
    code: `import hmac, hashlib
from flask import Flask, request, abort

def verify(raw_body: bytes, signature_header: str, secret: str) -> bool:
    expected = "sha256=" + hmac.new(
        secret.encode(), raw_body, hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, signature_header or "")

@app.post("/webhooks/vodstack")
def webhook():
    # request.data = ham gövde (parse'tan önce)
    if not verify(request.data, request.headers.get("X-Vodstack-Signature"), WHSEC):
        abort(401)
    event = request.get_json()
    # ... event["event"]'e göre işle, hızlıca 200 dön ...
    return "", 200`,
  },
]

// ---------------------------------------------------------------------------
// Sayfa
// ---------------------------------------------------------------------------

export function DocsPage() {
  return (
    <div className="mx-auto max-w-6xl">
      {/* Hero */}
      <div className="relative mb-10 overflow-hidden rounded-3xl border border-edge bg-graphite/60 p-8 shadow-deck">
        <div className="pointer-events-none absolute -right-16 -top-20 h-64 w-64 rounded-full bg-signal/10 blur-3xl" />
        <div className="relative">
          <div className="eyebrow mb-3 flex items-center gap-2">
            <BookOpen className="h-3.5 w-3.5" /> geliştirici kılavuzu
          </div>
          <h1 className="font-display text-3xl font-semibold tracking-tight text-chalk sm:text-4xl">
            Entegrasyon
          </h1>
          <p className="mt-3 max-w-2xl text-sm leading-relaxed text-haze">
            vodstack’i kendi uygulamanıza bağlamak için API anahtarı ve webhook
            kılavuzu. Anahtarları{' '}
            <span className="text-chalk">API Anahtarları</span>, endpoint’leri{' '}
            <span className="text-chalk">Webhook’lar</span> sayfasından yönetirsiniz;
            bu sayfa nasıl kullanacağınızı anlatır.
          </p>
          {/* Hızlı referans */}
          <div className="mt-6 grid gap-3 sm:grid-cols-3">
            <div className="rounded-xl border border-edge bg-ink/50 p-3">
              <div className="eyebrow mb-1">base url</div>
              <code className="font-mono text-xs text-chalk">/api/library/&#123;libraryId&#125;</code>
            </div>
            <div className="rounded-xl border border-edge bg-ink/50 p-3">
              <div className="eyebrow mb-1">kimlik</div>
              <code className="font-mono text-xs text-chalk">Authorization: Bearer vds_…</code>
            </div>
            <div className="rounded-xl border border-edge bg-ink/50 p-3">
              <div className="eyebrow mb-1">tam şema</div>
              <code className="font-mono text-xs text-chalk">deploy/openapi.yaml</code>
            </div>
          </div>
        </div>
      </div>

      <div className="gap-12 xl:grid xl:grid-cols-[minmax(0,1fr)_200px]">
        <div className="min-w-0 space-y-14">
          {/* A. Hızlı başlangıç */}
          <Section
            id="baslangic"
            kicker="adım adım"
            title="Hızlı başlangıç"
            icon={<Workflow className="h-5 w-5" />}
          >
            <p className="text-sm leading-relaxed text-haze">
              Tipik bir video yaşam döngüsü: <Inline>oluştur</Inline> →{' '}
              <Inline>upload-url</Inline> → <Inline>PUT</Inline> →{' '}
              <Inline>complete</Inline> → durumu yokla → <Inline>play</Inline>.
            </p>

            {/* Durum makinesi */}
            <div className="flex flex-wrap items-center gap-1.5">
              {STATUS_STEPS.map((s, idx) => (
                <span key={s.n} className="flex items-center gap-1.5">
                  <span
                    className={`inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1 font-mono text-[11px] ${
                      s.n === 4
                        ? 'border-ok/40 bg-ok/10 text-ok'
                        : s.n === 5
                          ? 'border-bad/40 bg-bad/10 text-bad'
                          : 'border-edge bg-ink/40 text-haze'
                    }`}
                  >
                    <span className="opacity-60">{s.n}</span> {s.label}
                  </span>
                  {idx < STATUS_STEPS.length - 2 && <span className="text-edge">→</span>}
                  {idx === STATUS_STEPS.length - 2 && (
                    <span className="px-1 text-[10px] text-haze/50">veya</span>
                  )}
                </span>
              ))}
            </div>
            <p className="text-xs text-haze/70">
              Durum tamsayıları Bunny ile birebir aynıdır — frontend’de yeniden
              eşleme gerekmez.
            </p>

            <CodeBlock code={FLOW_EXAMPLE} lang="bash" />
          </Section>

          {/* B. Kimlik doğrulama */}
          <Section
            id="kimlik"
            kicker="api anahtarları"
            title="Kimlik doğrulama"
            icon={<KeyRound className="h-5 w-5" />}
          >
            <ul className="space-y-2 text-sm leading-relaxed text-haze">
              <li>
                • Anahtar <span className="text-chalk">API Anahtarları</span> sayfasından
                üretilir; düz metin (<Inline>vds_…</Inline>) sunucuda SHA-256 hash olarak
                saklanır.
              </li>
              <li>
                • Tüm istekler{' '}
                <Inline>/api/library/&#123;libraryId&#125;/…</Inline> altındadır.
              </li>
              <li>
                • Anahtarı iki başlıktan biriyle gönderin:{' '}
                <Inline>Authorization: Bearer vds_…</Inline> (önerilen) veya{' '}
                <Inline>AccessKey: vds_…</Inline> (Bunny uyumluluğu).
              </li>
            </ul>

            <Callout tone="warn" title="Anahtar yalnızca bir kez gösterilir">
              Oluşturma anında dönen düz-metin anahtar bir daha gösterilmez ve
              kurtarılamaz. Güvenli bir yerde saklayın; kaybederseniz yeni anahtar
              üretin.
            </Callout>

            <CodeTabs tabs={AUTH_TABS} />

            <Callout title="Rate limit">
              Anahtar başına varsayılan <span className="text-chalk">20 rps / 40 burst</span>.
              Aşımda <Inline>429</Inline> + <Inline>Retry-After: 1</Inline> döner; gövde{' '}
              <Inline>&#123;"error":"rate limit exceeded"&#125;</Inline>.
            </Callout>

            <div>
              <h3 className="mb-2 font-mono text-[11px] uppercase tracking-[0.18em] text-haze">
                Hata kodları
              </h3>
              <Table head={['Kod', 'Anlam']}>
                {ERRORS.map((e) => (
                  <tr key={e.code} className="border-b border-edge/50 last:border-0">
                    <td className="px-3 py-2 font-mono text-chalk">{e.code}</td>
                    <td className="px-3 py-2 text-haze">{e.meaning}</td>
                  </tr>
                ))}
              </Table>
              <p className="mt-2 font-mono text-[11px] text-haze/70">
                Hata gövdesi her zaman <Inline>&#123;"error":"…"&#125;</Inline> formatındadır.
              </p>
            </div>
          </Section>

          {/* C. Uç noktalar */}
          <Section
            id="uc-noktalar"
            kicker="rest api"
            title="Sık kullanılan uç noktalar"
            icon={<Plug className="h-5 w-5" />}
          >
            <p className="text-sm leading-relaxed text-haze">
              Hepsi <Inline>/api/library/&#123;libraryId&#125;</Inline> ön ekiyle ve API
              anahtarı ile çağrılır. Tam istek/yanıt şeması için{' '}
              <Inline>deploy/openapi.yaml</Inline>.
            </p>
            <Panel className="divide-y divide-edge/50">
              {ENDPOINTS.map((e) => (
                <div key={e.method + e.path} className="flex items-center gap-3 px-4 py-2.5">
                  <Method m={e.method} />
                  <code className="shrink-0 font-mono text-xs text-chalk">{e.path}</code>
                  <span className="ml-auto text-right text-xs text-haze">{e.desc}</span>
                </div>
              ))}
            </Panel>
          </Section>

          {/* D. Webhook'lar */}
          <Section
            id="webhooks"
            kicker="olay bildirimleri"
            title="Webhook'lar"
            icon={<Webhook className="h-5 w-5" />}
          >
            <ul className="space-y-2 text-sm leading-relaxed text-haze">
              <li>
                • Endpoint <span className="text-chalk">Webhook’lar</span> sayfasından
                eklenir. Hiç event seçilmezse{' '}
                <span className="text-chalk">tüm event’lere</span> abone olunur.
              </li>
              <li>
                • Teslimat: <Inline>POST</Inline>,{' '}
                <Inline>Content-Type: application/json</Inline>, 15 sn timeout. 2xx
                dışındaki yanıtlar üstel backoff ile ~10 kez yeniden denenir.
              </li>
              <li>
                • Başlıklar: <Inline>X-Vodstack-Event</Inline>,{' '}
                <Inline>X-Vodstack-Delivery</Inline>, <Inline>X-Vodstack-Signature</Inline>.
              </li>
            </ul>

            <Callout title="Idempotent teslimat">
              Aynı olay birden çok kez ulaşabilir. <Inline>X-Vodstack-Delivery</Inline> ID’sini
              kaydederek yinelenen teslimatları ayıklayın.
            </Callout>

            <div>
              <h3 className="mb-2 font-mono text-[11px] uppercase tracking-[0.18em] text-haze">
                Örnek istek
              </h3>
              <CodeBlock code={WEBHOOK_PAYLOAD} lang="http" />
            </div>

            <div>
              <h3 className="mb-2 font-mono text-[11px] uppercase tracking-[0.18em] text-haze">
                Event’ler
              </h3>
              <Table head={['Event', 'Ne zaman', 'data alanları']}>
                {WEBHOOK_EVENTS.map((ev) => {
                  const doc = EVENT_DOCS[ev]
                  return (
                    <tr key={ev} className="border-b border-edge/50 align-top last:border-0">
                      <td className="whitespace-nowrap px-3 py-2 font-mono text-[11px] text-signal">
                        {ev}
                      </td>
                      <td className="px-3 py-2 text-haze">{doc?.when ?? '—'}</td>
                      <td className="px-3 py-2 font-mono text-[11px] text-haze">
                        {doc?.data ?? '—'}
                      </td>
                    </tr>
                  )
                })}
              </Table>
            </div>
          </Section>

          {/* E. İmza doğrulama */}
          <Section
            id="imza"
            kicker="güvenlik"
            title="İmza doğrulama"
            icon={<ShieldCheck className="h-5 w-5" />}
          >
            <p className="text-sm leading-relaxed text-haze">
              Her isteğin <Inline>X-Vodstack-Signature</Inline> başlığı{' '}
              <Inline>sha256=&lt;hex&gt;</Inline> formatındadır:{' '}
              <span className="text-chalk">HMAC-SHA256(secret, ham gövde)</span>. Secret,
              endpoint oluşturulurken bir kez gösterilen <Inline>whsec_…</Inline> değeridir.
            </p>

            <Callout tone="warn" title="İmzayı ham gövde üzerinden hesaplayın">
              HMAC’i isteğin <span className="text-chalk">ham byte gövdesi</span> üzerinden,
              JSON’u parse etmeden önce hesaplayın ve sabit-zamanlı karşılaştırın
              (<Inline>timingSafeEqual</Inline> / <Inline>compare_digest</Inline>). Yeniden
              serialize edilmiş gövde farklı imza üretir.
            </Callout>

            <CodeTabs tabs={VERIFY_TABS} />
          </Section>
        </div>

        <Toc />
      </div>
    </div>
  )
}
