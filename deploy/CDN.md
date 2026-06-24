# Cloudflare CDN kurulumu (Cloudflare Tunnel ile)

vodstack, Cloudflare'e **proje ayarlarından** geçilecek şekilde hazır. Tünel
kullanıyoruz çünkü Cloudflare proxy origin'e yalnızca belirli portlardan bağlanır
(38082 desteklenmez) ve tünel: **dışarı port açmaz, origin IP'sini gizler, HTTPS'i
otomatik verir.**

```
İzleyici ─https─> Cloudflare (cache, %95+ hit) ─tünel─> edge:80 ─> origin ─> MinIO
                                               └────────> api:8080  (embed + AES key)
                                               └────────> web:80    (panel, opsiyonel)
```

## Önkoşul
- Cloudflare'de yönetilen bir alan adı (ör. `example.com`).

## 1) Tünel oluştur (token al)
Cloudflare **Zero Trust** paneli → **Networks → Tunnels → Create a tunnel** →
**Cloudflared** tipi → isim ver (ör. `vodstack`) → **token'ı kopyala**
(`eyJ...` ile başlayan uzun değer).

## 2) Public hostname'leri iç servislere bağla
Aynı tünelin **Public Hostname** sekmesinde şunları ekle (Service = compose ağı
içindeki servis adı):

| Subdomain | Type | URL (Service) | Karşılığı |
|---|---|---|---|
| `stream.example.com` | HTTP | `edge:80` | Oynatma (PUBLIC_BASE_URL) |
| `api.example.com` | HTTP | `api:8080` | Embed + AES key (EMBED/KEY_BASE_URL) |
| `panel.example.com` | HTTP | `web:80` | Admin paneli (opsiyonel) |

> Service kısmında `http://edge:80` yazman yeterli — cloudflared compose ağında
> olduğu için bu isimleri çözer.

## 3) Proje ayarları (`deploy/.env.prod`)
```ini
CLOUDFLARE_TUNNEL_TOKEN=eyJ...buraya...

PUBLIC_BASE_URL=https://stream.example.com
EMBED_BASE_URL=https://api.example.com
KEY_BASE_URL=https://api.example.com

# Tünel modunda hiçbir şeyin dışarı açılmasına gerek yok:
PUBLIC_IP=127.0.0.1
OPS_BIND=127.0.0.1
```
> `KEY_BASE_URL` videoyu **şifrelerken** playlist'e gömülür. Önce bunu doğru
> ayarla; eski şifreli videolar için panelden yeniden şifrele.

## 4) Başlat (cdn profili ile)
```bash
docker compose --env-file deploy/.env.prod --profile cdn \
  -f deploy/docker-compose.yml up --build -d
```
Tünel olmadan çalıştırmak istersen `--profile cdn` olmadan başlat — direkt moda
döner (eski sabit-IP davranışı).

## 5) ⭐ Cache Rule (en kritik adım)
Cloudflare medya segment'lerini (`.m4s`/`.ts`) **varsayılan olarak cache'lemez**
ve query string'i cache key'e katar (her izleyicinin token'ı farklı → %0 hit).
İkisini de bir Cache Rule ile düzelt:

Dashboard → ilgili site → **Caching → Cache Rules → Create rule**:
- **When**: `Hostname equals stream.example.com` **and** `URI Path starts with /hls/`
- **Then**:
  - **Cache eligibility** → *Eligible for cache*
  - **Edge TTL** → *Use cache-control header from origin* (segment'ler 1 yıl,
    playlist'ler 10 sn — başlıkları zaten doğru gönderiyoruz)
  - **Cache Key → Query String** → *Ignore query string* (token'ı cache key'den
    çıkarır; token origin'e yine iletilir, edge doğrular)

## 6) Doğrula
Bir oynatma URL'sini iki kez çek; ikincisinde Cloudflare'den gelmeli:
```bash
curl -sI "https://stream.example.com/hls/<id>/master.m3u8?exp=...&token=..." | grep -i cf-cache-status
# 1.: cf-cache-status: MISS   2.: HIT
```

## Önemli notlar
- **Güvenlik tradeoff'u:** Cache Rule query'yi yok saydığı için token yalnızca
  **cache MISS**'te (origin'e gidildiğinde) doğrulanır. Bir segment Cloudflare'de
  cache'lendikten sonra, path'i bilen biri geçerli token olmadan da çekebilir.
  Koruma = tahmin edilemez `videoId` (projenin sidecar'larda kullandığı model).
  Eğitim VOD'u için yeterli; kişi-bazlı sıkı koruma gerekiyorsa Cloudflare'in
  kendi imzalı-URL/Token Authentication'ına geçmek gerekir (ayrı iş).
- **Cloudflare ToS:** Self-serve (Free/Pro/Business) planlarda "orantısız" video
  trafiği servis etmek ToS'a takılabilir; yoğun hacimde Cloudflare seni
  yavaşlatabilir ya da Stream'e/Enterprise'a yönlendirebilir. Küçük HLS
  segment'leri çoğu durumda sorunsuz çalışır ama hacim büyürse bunu göz önünde tut.
- **Mixed content çözüldü:** Tünel her şeyi HTTPS yaptığı için `KEY_BASE_URL` ve
  `EMBED_BASE_URL` da `https://` olur → AES key/embed çağrıları engellenmez.
- **Origin gizli:** Tünel outbound-only olduğu için sunucunun IP'sini kimse
  göremez; istersen sunucuda 80/443'ü tamamen kapatabilirsin (sadece SSH kalsın).
