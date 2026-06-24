# Yük testi — eşzamanlı izleyici (HLS playback)

Bu klasör, vodstack'in **playback** düzlemini (gerçek izleyici yükü) test eder.
Kontrol düzlemi (API throughput) ve upload/transcode kapasitesi ayrı senaryolar
gerektirir — onlar için aşağıdaki nota bak.

## Neden ayrı?

Bir izleyici API'ye tek istek atar (`/play`), gerisini **edge nginx → origin →
MinIO** zincirinden segment olarak çeker. Asıl yük (RPS + bant genişliği) burada.
Klasik bir REST yük testi bu zinciri hiç görmez.

## Kurulum (Windows)

```powershell
winget install k6   # veya: choco install k6
```

## Hazırlık

Staging'e **zaten işlenmiş (ready) en az bir video** yükle. ID'sini ve library
ID'sini al (panelde video detayında ya da `/api/library/{lib}/videos`).

## Çalıştırma

Tek video, 200 eşzamanlı izleyici, her biri 120 sn izlesin:

```powershell
k6 run -e BASE_URL=https://staging.example.com `
       -e LIBRARY_ID=<lib> -e VIDEO_ID=<vid> `
       -e VUS=200 -e WATCH_SECONDS=120 playback.js
```

## İki farklı şey ölçülür — hangisini istediğine karar ver

1. **Edge cache + bant genişliği (tek/az video):** Tüm izleyiciler aynı
   videoyu çeker. İlk izleyiciden sonra segmentler edge NVMe cache'inden gelir.
   Bu, "viral tek video" / "canlı ders" senaryosudur. Segment latency çok düşük
   (ms) olmalı; darboğaz **dışa bant genişliği**.

2. **Origin + MinIO (çok video, cache miss):** Her izleyici farklı video çeker →
   edge cache çoğunlukla ıskalar → istek origin nginx ve MinIO'ya iner. Gerçek
   "katalog" yükü. Darboğaz **MinIO IOPS + origin CPU + disk**.

   ```powershell
   k6 run -e BASE_URL=https://staging.example.com `
          -e LIBRARY_ID=<lib> -e VIDEO_IDS=id1,id2,id3,id4,id5 `
          -e VUS=500 playback.js
   ```

İkisini de çalıştır — biri cache'i, diğeri origin'i sınar.

## Önemli parametreler

| Değişken         | Varsayılan | Açıklama |
|------------------|-----------|----------|
| `VUS`            | 100       | Eşzamanlı izleyici sayısı |
| `WATCH_SECONDS`  | 60        | Her izleyici kaç saniye izlesin |
| `REALTIME`       | 1         | 1: gerçek-zamanlı (segment başına bekle). 0: olabildiğince hızlı çek (bant genişliği stresi — izleyici sayısı anlamını yitirir) |
| `ABR`            | top       | Hangi kalite: `top`/`mid`/`bottom`/`random` |
| `RAMP` / `HOLD`  | 1m / 3m   | Rampa ve plato süreleri |

## Test SIRASINDA neyi izle (asıl iş bu)

k6 çıktısı sadece istemci tarafını gösterir. Sunucu tarafında şunlara bak:

- **Edge nginx:** cache hit oranı (`cf-cache-status` veya nginx `$upstream_cache_status`),
  aktif bağlantı, dışa bant genişliği. Asıl rakam burada.
- **`/metrics`** (API ve worker Prometheus endpoint'leri) — istek süreleri, hata oranı.
- **MinIO:** CPU, disk IOPS, ağ. Cache-miss senaryosunda ilk patlar.
- **Postgres:** `/play` her izleyicide `GetVideo` + `GetAccess` çağırıyor →
  bağlantı havuzu (`max_connections`) ve aktif bağlantı sayısı.
- **Rate limiter:** kütüphane API'sini test ediyorsan `RateLimitRPS`/`RateLimitBurst`
  429 dönebilir. (Embed `/play` ve `/beacon` API-key'siz, ayrı yolda — onlar
  IP-rate-limit'li.) `/beacon` IP başına limit, tek makineden test ederken
  yapay 429'lara dikkat — gerçek dağılımı yansıtmaz.
- **CPU/RAM:** `docker stats` ile tüm konteynerler.

## Yorumlama

- `first_frame_ms` p95 > 4 sn → izleyici "yüklenmiyor" der. Genelde `/play`
  (Postgres) ya da master indirme yavaş.
- `segment_request_ms` p95 cache-hit'te ms mertebesinde olmalı; yüksekse cache
  ıskalıyor ya da MinIO/origin doymuş.
- `segment_errors` / `http_req_failed` artıyorsa: token süresi (`exp`) testten
  kısa olabilir, ya da gerçekten kapasite bitti.

## Diğer iki düzlem (bu klasör kapsamı dışında, ihtiyaç olursa ekle)

- **Control-plane API:** `/api/library/{lib}/...` JSON endpoint'leri — k6 ile
  API-key başlığı vererek; `RateLimitRPS` limitini ve Postgres havuzunu sınar.
- **Upload + transcode:** birkaç büyük dosyayı tus ile eşzamanlı yükle, worker
  kuyruğunu (Redis) ve ffmpeg CPU'sunu/scratch diskini izle. Tek sunucu +
  GPU-suz kısıt nedeniyle asıl tıkanma genelde burada olur.
```

