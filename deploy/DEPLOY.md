# vodstack — sabit IP / HTTP üretim kurulumu

Bu kurguda **sadece 3 servis** sunucunun sabit IP'sinde dışarı açılır; gerisi
sunucu-içi (veya yalnızca SSH tüneli) kalır.

Portlar, sunucudaki diğer stack'lerle (redis `6379`, open-webui `1453`,
rabbitmq `15672`, mongo `27017`, seq `5341`) çakışmaması için ayrı bir
`38xxx/39xxx` bloğuna taşındı. Hepsi `.env.prod` içinden değiştirilebilir.

| Servis | Host portu | Erişim | Not |
|---|---|---|---|
| edge | `38082` | **Public** (FIXED_IP) | İzleyici HLS oynatma (token zorunlu) |
| web (panel) | `38090` | **Public** (FIXED_IP) | Yönetim paneli |
| api | `38080` | **Public** (FIXED_IP) | Embed + public library API + AES anahtarı |
| minio | `39000/39001` | `127.0.0.1` | Sadece SSH tüneli (yükleme panelden akar) |
| origin | `38081` | `127.0.0.1` | Debug |
| prometheus | `39092` | `127.0.0.1` | SSH tüneli |
| grafana | `33001` | `127.0.0.1` | SSH tüneli |
| postgres / redis / whisper | — | yok | Hiç host portu açmaz; sunucudaki redis 6379 ile çakışmaz |

## Gereksinimler
- Linux sunucu (sabit/public IP), Docker + Docker Compose v2
- ~4+ CPU çekirdek, 8GB+ RAM (ffmpeg + whisper CPU-bound), videolar için disk

## Adımlar

```bash
# 1) Repoyu sunucuya kopyala, dizine gir
cd vodstack

# 2) Env dosyasını oluştur ve doldur (FIXED_IP + tüm secret'lar)
cp deploy/.env.prod.example deploy/.env.prod
#   - FIXED_IP, PUBLIC_BASE_URL/EMBED_BASE_URL/KEY_BASE_URL içindeki IP
#   - TOKEN_SECRET (openssl rand -hex 32), ADMIN_PASSWORD, PG/MINIO/GRAFANA şifreleri
nano deploy/.env.prod

# 3) Başlat
docker compose --env-file deploy/.env.prod -f deploy/docker-compose.yml up --build -d

# 4) Durum
docker compose --env-file deploy/.env.prod -f deploy/docker-compose.yml ps
```

Açılınca:
- Panel  → `http://<FIXED_IP>:38090` (login: `ADMIN_PASSWORD`)
- Oynatma → `http://<FIXED_IP>:38082` (imzalı URL ile)
- API    → `http://<FIXED_IP>:38080`

> **Önemli:** `PUBLIC_BASE_URL`, `EMBED_BASE_URL`, `KEY_BASE_URL` içindeki IP
> tarayıcıya gömülür. `localhost` veya yanlış IP kalırsa oynatma/embed kırılır.
> `TOKEN_SECRET` api ve edge için **aynı** olmak zorunda (tek değişkenden besleniyor).

## Ops arayüzlerine erişim (SSH tüneli)
MinIO/Grafana/Prometheus dışarı kapalı. Yerel makinenden:

```bash
ssh -L 39001:127.0.0.1:39001 -L 33001:127.0.0.1:33001 -L 39092:127.0.0.1:39092 user@<FIXED_IP>
# sonra tarayıcıda: http://localhost:39001 (MinIO) · http://localhost:33001 (Grafana)
```

## Güvenlik duvarı (opsiyonel ama önerilir)
Docker yayınlanmış portları doğrudan iptables ile açar (ufw'yi baypas edebilir).
`deploy/firewall.sh` SSH + sadece public portları (38080/38082/38090) açıp
gerisini kapatır:

```bash
sudo bash deploy/firewall.sh
```

## Güncelleme
```bash
git pull
docker compose --env-file deploy/.env.prod -f deploy/docker-compose.yml up --build -d
```

## Yedek
```bash
docker compose --env-file deploy/.env.prod -f deploy/docker-compose.yml run --rm backup
```

## Cloudflare CDN + HTTPS (önerilen, 3-5 bin+ için)
Bant genişliğini Cloudflare'e devretmek (ve otomatik HTTPS almak) için
**Cloudflare Tunnel** kurulumu hazır: `deploy/CDN.md`. Özetle: tüneli aç,
`.env.prod`'da `CLOUDFLARE_TUNNEL_TOKEN` + üç URL'yi Cloudflare hostname'lerine
ayarla, `--profile cdn` ile başlat. Tünel modunda hiçbir port dışarı açılmaz
(`PUBLIC_IP=127.0.0.1`), origin IP gizli kalır.

## Dev (değişmedi)
Env dosyası vermeden çalıştırırsan eski dev davranışı birebir korunur:
```bash
docker compose -f deploy/docker-compose.yml up --build
```
