// vodstack — HLS playback load test (k6)
//
// Her sanal kullanıcı (VU) GERÇEK bir izleyiciyi taklit eder:
//   1) GET /embed/{lib}/{vid}/play     -> imzalı master.m3u8 (?exp&token)
//   2) GET master.m3u8                 -> variant playlist'i seç (ABR)
//   3) GET variant playlist            -> init segment + .m4s segment listesi
//   4) segmentleri GERÇEK ZAMANLI çek  -> her ~segment-süresi kadar bekle
//   5) POST /beacon                    -> start/progress/ended event'leri
//
// KRİTİK: Gerçek bir oynatıcı tüm videoyu anında indirmez; saniyede bir
// segment çeker. Bu yüzden VU sayısı = EŞZAMANLI İZLEYİCİ sayısıdır ve her VU
// kendini segment süresi kadar bekleterek frenler (REALTIME=1, varsayılan).
// Bunu kapatırsan (REALTIME=0) bant genişliği/origin'i zorlarsın ama izleyici
// sayısı anlamını yitirir.
//
// Çalıştırma örnekleri:
//   k6 run -e BASE_URL=https://staging.example.com \
//          -e LIBRARY_ID=<lib> -e VIDEO_ID=<vid> \
//          -e VUS=200 -e WATCH_SECONDS=120 loadtest/playback.js
//
//   # birden çok videoyu dağıt (edge cache'i ASMAK / origin+MinIO'yu zorlamak için):
//   k6 run -e VIDEO_IDS=id1,id2,id3,... -e VUS=500 loadtest/playback.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Counter, Rate } from 'k6/metrics';
import { randomItem } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

// ---- yapılandırma (ortam değişkenleriyle) ----
const BASE_URL = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const LIBRARY_ID = __ENV.LIBRARY_ID || '';
const VIDEO_IDS = (__ENV.VIDEO_IDS || __ENV.VIDEO_ID || '').split(',').map(s => s.trim()).filter(Boolean);
const WATCH_SECONDS = Number(__ENV.WATCH_SECONDS || 60);   // her izleyici kaç sn izlesin
const REALTIME = (__ENV.REALTIME || '1') !== '0';          // segment başına gerçek-zamanlı bekle
const ABR = (__ENV.ABR || 'top');                          // top | mid | bottom | random — hangi kaliteyi seçsin

if (!LIBRARY_ID || VIDEO_IDS.length === 0) {
  throw new Error('LIBRARY_ID ve VIDEO_ID (veya VIDEO_IDS) ortam değişkenleri zorunlu');
}

// ---- özel metrikler ----
const ttff = new Trend('first_frame_ms', true);      // play çağrısı + ilk segment = "başlama gecikmesi"
const playReq = new Trend('play_request_ms', true);
const segReq = new Trend('segment_request_ms', true);
const segBytes = new Counter('segment_bytes');
const segErrors = new Rate('segment_errors');
const startupErrors = new Rate('startup_errors');

// ---- yük profili: kademeli ramp (staging'i bir anda boğma) ----
const VUS = Number(__ENV.VUS || 100);
export const options = {
  scenarios: {
    viewers: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: __ENV.RAMP || '1m', target: VUS },     // yavaşça çık
        { duration: __ENV.HOLD || '3m', target: VUS },     // platoda tut
        { duration: '30s', target: 0 },                    // soğut
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    // Bu eşikleri AŞARSA test "başarısız" sayılır — kendi SLO'na göre ayarla.
    'first_frame_ms': ['p(95)<4000'],        // %95 izleyici 4 sn içinde başlasın
    'segment_request_ms': ['p(95)<2000'],    // segmentler hızlı gelsin (cache hit ~ms olmalı)
    'segment_errors': ['rate<0.01'],         // <%1 segment hatası
    'startup_errors': ['rate<0.01'],
    'http_req_failed': ['rate<0.02'],
  },
};

// master/variant playlist'ten URI satırlarını ayıkla (# ile başlamayanlar URI).
function parsePlaylist(body) {
  const lines = body.split('\n').map(l => l.trim()).filter(Boolean);
  const variants = [];   // master için: variant playlist URI'leri (BANDWIDTH ile)
  const segments = [];   // media playlist için: segment URI'leri
  let initUri = null;    // EXT-X-MAP (fMP4 init segment)
  let targetDur = 6;     // EXT-X-TARGETDURATION (gerçek-zamanlı pacing için)
  let pendingBw = 0;

  for (let i = 0; i < lines.length; i++) {
    const l = lines[i];
    if (l.startsWith('#EXT-X-STREAM-INF')) {
      const m = l.match(/BANDWIDTH=(\d+)/);
      pendingBw = m ? Number(m[1]) : 0;
      continue;
    }
    if (l.startsWith('#EXT-X-MAP')) {
      const m = l.match(/URI="([^"]+)"/);
      if (m) initUri = m[1];
      continue;
    }
    if (l.startsWith('#EXT-X-TARGETDURATION')) {
      const m = l.match(/:(\d+(?:\.\d+)?)/);
      if (m) targetDur = Number(m[1]);
      continue;
    }
    if (l.startsWith('#')) continue;
    // URI satırı
    if (pendingBw) { variants.push({ uri: l, bw: pendingBw }); pendingBw = 0; }
    else segments.push(l);
  }
  return { variants, segments, initUri, targetDur };
}

// göreli URI'yi master'ın URL'sine göre çöz + token query'sini ekle
// (oynatıcı da aynısını yapıyor: child istekleri master'ın ?exp&token'ını taşır).
function resolve(baseUrl, tokenQuery, uri) {
  let abs;
  if (/^https?:\/\//.test(uri)) abs = uri;
  else if (uri.startsWith('/')) { const u = new URL(baseUrl); abs = u.origin + uri; }
  else { abs = baseUrl.substring(0, baseUrl.lastIndexOf('/') + 1) + uri; }
  abs = abs.split('?')[0]; // varsa kendi query'sini at, master'ınkini koy
  if (tokenQuery) abs += '?' + tokenQuery;
  return abs;
}

function pickVariant(variants) {
  if (variants.length <= 1) return variants[0];
  const sorted = variants.slice().sort((a, b) => a.bw - b.bw);
  if (ABR === 'bottom') return sorted[0];
  if (ABR === 'mid') return sorted[Math.floor(sorted.length / 2)];
  if (ABR === 'random') return randomItem(sorted);
  return sorted[sorted.length - 1]; // 'top'
}

export default function () {
  const videoId = randomItem(VIDEO_IDS);
  const sessionId = `${__VU}-${__ITER}-${Date.now()}`;
  const t0 = Date.now();

  // 1) play payload (public embed endpoint — auth yok)
  const playRes = http.get(`${BASE_URL}/embed/${LIBRARY_ID}/${videoId}/play`, {
    tags: { name: 'play' },
  });
  playReq.add(playRes.timings.duration);
  if (!check(playRes, { 'play 200': r => r.status === 200 })) {
    startupErrors.add(1); return;
  }
  let payload;
  try { payload = playRes.json(); } catch (e) { startupErrors.add(1); return; }
  const hlsUrl = payload.hlsUrl;
  if (!hlsUrl) { startupErrors.add(1); return; } // henüz hazır değil / hata
  const tokenQuery = hlsUrl.split('?')[1] || '';

  // 2) master playlist
  const masterRes = http.get(hlsUrl, { tags: { name: 'master.m3u8' } });
  if (!check(masterRes, { 'master 200': r => r.status === 200 })) { startupErrors.add(1); return; }
  const master = parsePlaylist(masterRes.body);
  const variant = pickVariant(master.variants);
  if (!variant) { startupErrors.add(1); return; }

  // 3) variant (media) playlist
  const variantUrl = resolve(hlsUrl, tokenQuery, variant.uri);
  const mediaRes = http.get(variantUrl, { tags: { name: 'variant.m3u8' } });
  if (!check(mediaRes, { 'variant 200': r => r.status === 200 })) { startupErrors.add(1); return; }
  const media = parsePlaylist(mediaRes.body);

  // 4) init segment (fMP4)
  if (media.initUri) {
    const initUrl = resolve(variantUrl, tokenQuery, media.initUri);
    const r = http.get(initUrl, { tags: { name: 'init.mp4' } });
    segReq.add(r.timings.duration);
    if (r.body) segBytes.add(r.body.length);
    check(r, { 'init 200': x => x.status === 200 });
  }

  // 5) segmentleri gerçek-zamanlı çek
  beacon(videoId, sessionId, 'start', 0, payload.vt);
  const segDur = media.targetDur || 6;
  const maxSegs = Math.max(1, Math.ceil(WATCH_SECONDS / segDur));
  let firstFrameRecorded = false;

  for (let i = 0; i < Math.min(maxSegs, media.segments.length); i++) {
    const segUrl = resolve(variantUrl, tokenQuery, media.segments[i]);
    const r = http.get(segUrl, { tags: { name: 'segment.m4s' } });
    segReq.add(r.timings.duration);
    segErrors.add(r.status !== 200);
    if (r.body) segBytes.add(r.body.length);

    if (!firstFrameRecorded) {
      // ilk segment indi = ilk kare gösterilebilir => başlama gecikmesi
      ttff.add(Date.now() - t0);
      firstFrameRecorded = true;
    }

    // her ~5 segmentte bir ilerleme beacon'u (oynatıcı 5 sn'de bir atıyor)
    if (i > 0 && i % 5 === 0) beacon(videoId, sessionId, 'progress', i * segDur, payload.vt);

    // GERÇEK ZAMANLI: oynatıcı bir sonraki segmenti ~segDur sn sonra ister.
    if (REALTIME) sleep(segDur);
  }

  beacon(videoId, sessionId, 'ended', maxSegs * segDur, payload.vt);
}

function beacon(videoId, sessionId, event, position, vt) {
  const body = { videoId, libraryId: LIBRARY_ID, sessionId, event, position };
  if (vt) body.vt = vt;
  http.post(`${BASE_URL}/beacon`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'beacon' },
  });
}
