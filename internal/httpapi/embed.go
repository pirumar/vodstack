package httpapi

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/player"
)

// handleEmbedPlay is the PUBLIC play endpoint the embeddable player calls. It
// mints a signed play payload without an API key or session — it is meant to be
// hit from a third-party page inside the iframe. (Per-video access control —
// public/signed/private, referrer locking — arrives in Phase 5; until then any
// existing, ready video is embeddable.)
func (s *Server) handleEmbedPlay(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "libraryId")
	videoID := chi.URLParam(r, "videoId")

	v, err := s.db.GetVideo(r.Context(), libraryID, videoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// Enforce the per-video access policy before minting a public play token.
	if acc, err := s.db.GetAccess(r.Context(), libraryID, videoID); err == nil {
		if acc.ExpiresAt != nil && time.Now().After(*acc.ExpiresAt) {
			writeError(w, http.StatusForbidden, "video access expired")
			return
		}
		if acc.Visibility != "public" && !referrerAllowed(acc.AllowedReferrers, r) {
			writeError(w, http.StatusForbidden, "video is not publicly embeddable")
			return
		}
	}

	// CORS-open: the player page is same-origin with the API, but allowing this
	// keeps it usable if served from elsewhere.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, http.StatusOK, s.playResponse(r.Context(), v))
}

// referrerAllowed reports whether the request's Origin/Referer host matches the
// allowlist. Best-effort (a determined caller can spoof headers); an empty
// allowlist on a non-public video denies. Note: for an <iframe> embed the Referer
// is the iframe (embed) origin, so referrer locking mainly gates direct/hotlink
// use — true parent-origin enforcement would need a signed embed parameter.
func referrerAllowed(allowed []string, r *http.Request) bool {
	if len(allowed) == 0 {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				origin = u.Scheme + "://" + u.Host
			}
		}
	}
	if origin == "" {
		return false
	}
	for _, a := range allowed {
		a = strings.TrimRight(strings.TrimSpace(a), "/")
		if a != "" && (origin == a || strings.HasPrefix(origin, a)) {
			return true
		}
	}
	return false
}

// embedData is the template payload for the iframe player page.
type embedData struct {
	LibraryID  string
	VideoID    string
	VT         string       // optional signed viewer token (passed through, verified downstream)
	Config     template.JS  // player.Config marshaled to a JS object literal
	CustomCSS  template.CSS // library-supplied CSS, sanitized (no markup escape)
	ShowSearch bool         // in-player transcript search box (search_config.ShowInPlayer)
}

// handleEmbed serves the self-contained iframe player page. It loads the
// vidstack player from a CDN and fetches the signed play payload from
// handleEmbedPlay. The library's player customization is injected as a JS config
// object; the token loader (re-append the master's ?exp&token to every child
// request) and the 403-refresh-and-resume logic mirror the panel's PlayerModal.
func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "libraryId")
	videoID := chi.URLParam(r, "videoId")

	cfg, err := s.db.GetPlayerConfig(r.Context(), libraryID)
	if err != nil {
		cfg = player.DefaultConfig()
	}
	cfgJSON, _ := json.Marshal(cfg)

	// In-player transcript search is available only when the library enabled both
	// search and the in-player box.
	showSearch := false
	if sc, err := s.db.GetSearchConfig(r.Context(), libraryID); err == nil {
		showSearch = sc.Enabled && sc.ShowInPlayer
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Allow this page to be framed by any site (that is the whole point).
	w.Header().Set("X-Frame-Options", "ALLOWALL")
	_ = embedTmpl.Execute(w, embedData{
		LibraryID:  libraryID,
		VideoID:    videoID,
		VT:         r.URL.Query().Get("vt"),
		Config:     template.JS(cfgJSON),
		CustomCSS:  template.CSS(cfg.CustomCSS),
		ShowSearch: showSearch,
	})
}

// embedTmpl is the vidstack-based iframe player. The library's player config is
// injected as the JS object CFG ({{.Config}}); {{.LibraryID}}/{{.VideoID}} are
// injected as JS strings; {{.CustomCSS}} is the library's sanitized CSS.
//
// It uses vidstack's official CDN + high-level VidstackPlayer.create() API
// (cdn.vidstack.io/player). We load hls.js ourselves and point vidstack's HLS
// provider at it (provider.library + provider.config.loader) so the existing
// token loader (re-append ?exp&token to every child request) and 403-refresh
// logic carry over unchanged on top of the signed-URL + edge-HMAC system. The
// provider-change listener is attached to the created player BEFORE the source
// is set, so the loader is in place before the first manifest request.
var embedTmpl = template.Must(template.New("embed").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>vodstack player</title>
<link rel="stylesheet" href="https://cdn.vidstack.io/player/theme.css" />
<link rel="stylesheet" href="https://cdn.vidstack.io/player/video.css" />
<style>
  html,body{margin:0;height:100%;background:#000;overflow:hidden;}
  #target{width:100%;height:100%;}
  media-player{width:100%;height:100%;}
  #msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
       color:#bbb;font-family:system-ui,sans-serif;font-size:.9rem;z-index:5;pointer-events:none;}
  /* Watchtime heatmap overlay (rendered just above the time slider). */
  .fz-heatmap{position:absolute;left:0;right:0;bottom:100%;height:30px;display:flex;
       align-items:flex-end;gap:1px;pointer-events:none;opacity:.85;}
  .fz-heatmap span{flex:1 1 0;background:var(--media-brand,#fab027);min-height:1px;border-radius:1px 1px 0 0;}
  /* In-player transcript search */
  .fz-search{position:absolute;top:10px;left:10px;z-index:7;font-family:system-ui,sans-serif;}
  .fz-search .fz-sbtn{width:34px;height:34px;border:0;border-radius:8px;cursor:pointer;
       background:rgba(0,0,0,.55);color:#fff;font-size:15px;line-height:34px;backdrop-filter:blur(4px);}
  .fz-sbox{margin-top:6px;width:280px;max-width:70vw;background:rgba(18,18,20,.94);
       border:1px solid rgba(255,255,255,.12);border-radius:10px;padding:8px;backdrop-filter:blur(8px);}
  .fz-sbox input{width:100%;box-sizing:border-box;padding:7px 9px;border-radius:7px;border:1px solid rgba(255,255,255,.15);
       background:#000;color:#fff;font-size:13px;outline:none;}
  .fz-results{margin-top:6px;max-height:220px;overflow-y:auto;display:flex;flex-direction:column;gap:4px;}
  .fz-results button{display:flex;gap:8px;align-items:flex-start;text-align:left;border:0;cursor:pointer;
       background:rgba(255,255,255,.05);color:#ccc;border-radius:7px;padding:6px 8px;font-size:12px;line-height:1.35;}
  .fz-results button:hover{background:rgba(255,255,255,.12);color:#fff;}
  .fz-results .fz-ts{flex:none;color:var(--media-brand,#fab027);font-variant-numeric:tabular-nums;font-weight:600;}
</style>
<style id="fz-custom">{{.CustomCSS}}</style>
</head>
<body>
<div id="target"></div>
<div id="msg">loading…</div>
<div id="fz-search" class="fz-search" style="display:none">
  <button class="fz-sbtn" id="fz-stoggle" title="Videoda ara" aria-label="Videoda ara">&#128269;</button>
  <div class="fz-sbox" id="fz-sbox" style="display:none">
    <input id="fz-sinput" type="text" placeholder="videoda ara…" />
    <div class="fz-results" id="fz-results"></div>
  </div>
</div>
<script src="https://cdn.jsdelivr.net/npm/hls.js@1.5.17/dist/hls.min.js"></script>
<script type="module">
import { VidstackPlayer, VidstackPlayerLayout } from 'https://cdn.vidstack.io/player';

var CFG = {{.Config}};
var LIB = "{{.LibraryID}}", VID = "{{.VideoID}}", VT = "{{.VT}}";
var SHOW_SEARCH = {{.ShowSearch}};
var base = "/embed/" + encodeURIComponent(LIB) + "/" + encodeURIComponent(VID);
var playURL = base + "/play";
var heatmapURL = base + "/heatmap";
var searchURL = base + "/search";
var progressURL = VT ? (base + "/progress?vt=" + encodeURIComponent(VT)) : "";
var POSKEY = "fz:pos:" + VID;
// Preview mode (the admin settings page): autoplay muted so the styled control
// bar is immediately visible and config changes are obvious at a glance.
var PREVIEW = /[?&]preview=1/.test(location.search || '');
var msg = document.getElementById('msg');
var tokenQuery = '', pendingSeek = 0, started = false, loadStart = 0;
var SID = (Math.random().toString(36).slice(2) + Date.now().toString(36)); // playback session (ephemeral, per page load)
// Persistent visitor id: identifies the same browser across reloads/sessions so
// analytics counts unique viewers, not page loads. Stored in localStorage (the
// embed is a third-party iframe, where third-party cookies are blocked). Falls
// back to the ephemeral SID when storage is unavailable (private mode / blocked).
var VIDKEY = "fz:vid";
var VISITOR = (function(){
  try {
    var v = localStorage.getItem(VIDKEY);
    if(!v){
      v = (window.crypto && crypto.randomUUID) ? crypto.randomUUID()
          : (Math.random().toString(36).slice(2) + Date.now().toString(36));
      localStorage.setItem(VIDKEY, v);
    }
    return v;
  } catch(e){ return SID; }
})();
var player = null;

// Turkish UI dictionary (unknown languages fall back to vidstack's English).
var TR = {
  "Play":"Oynat","Pause":"Duraklat","Mute":"Sessiz","Unmute":"Sesi aç",
  "Settings":"Ayarlar","Speed":"Hız","Normal":"Normal","Quality":"Kalite","Auto":"Otomatik",
  "Captions":"Altyazılar","Enable captions":"Altyazıları aç","Disable captions":"Altyazıları kapat",
  "Enter Fullscreen":"Tam ekran","Exit Fullscreen":"Tam ekrandan çık",
  "Enter PiP":"Resim içinde resim","Exit PiP":"PiP'den çık",
  "Seek":"Sar","Seek Forward":"İleri sar","Seek Backward":"Geri sar",
  "Volume":"Ses","Current Time":"Geçen süre","Duration":"Süre","Chapters":"Bölümler",
  "AirPlay":"AirPlay","Google Cast":"Google Cast","Continue":"Devam et","Replay":"Tekrar oynat",
  "Forward":"İleri","Rewind":"Geri","Audio":"Ses","Accessibility":"Erişilebilirlik"
};

// Hide every control NOT present in CFG.controls. vidstack's default layout
// renders these element tags inside <media-player>.
var SELECTORS = {
  playPause:"media-play-button",
  seekBackward:'media-seek-button[seconds^="-"]',
  seekForward:'media-seek-button:not([seconds^="-"])',
  mute:"media-mute-button",
  volume:"media-volume-slider",
  currentTime:'media-time[type="current"]',
  duration:'media-time[type="duration"]',
  progress:"media-time-slider",
  captions:"media-caption-button",
  settings:"media-menu.vds-settings-menu",
  pip:"media-pip-button",
  airplay:"media-airplay-button",
  chromecast:"media-google-cast-button",
  fullscreen:"media-fullscreen-button",
  bigPlayButton:".vds-controls media-play-button.vds-big-play-button"
};
function applyControlVisibility(){
  var on = {}; (CFG.controls||[]).forEach(function(k){ on[k]=true; });
  var css = "";
  Object.keys(SELECTORS).forEach(function(k){
    if(!on[k]) css += "media-player " + SELECTORS[k] + "{display:none!important;}\n";
  });
  if(CFG.compactControls){
    css += "media-player{--media-button-size:34px;--media-slider-height:40px;--media-font-size:13px;}\n";
  }
  var st = document.createElement('style'); st.textContent = css; document.head.appendChild(st);
}

// vidstack's default desktop layout has NO 10s seek buttons, so when the
// library enables them we inject <media-seek-button>s either side of the play
// button in the bottom control group. Returns true once done. The play button
// only appears after the video layout renders (which is after the media loads,
// which is deferred while the tab is hidden), so watchForSeekButtons() drives
// this from a MutationObserver rather than guessing a timing.
var seekInjected = false;
function injectSeekButtons(){
  if(seekInjected) return true;
  var on = {}; (CFG.controls||[]).forEach(function(k){ on[k]=true; });
  if(!on.seekBackward && !on.seekForward){ seekInjected = true; return true; }
  var playBtn = document.querySelector('media-play-button');
  if(!playBtn) return false; // layout not rendered yet
  var anchor = playBtn;
  while(anchor.parentElement && anchor.parentElement.tagName.toLowerCase() !== 'media-controls-group'){
    anchor = anchor.parentElement;
  }
  var group = anchor.parentElement;
  if(!group) return false;
  function makeSeek(seconds, iconType){
    var b = document.createElement('media-seek-button');
    b.setAttribute('seconds', seconds); b.className = 'vds-button';
    var ic = document.createElement('media-icon'); ic.setAttribute('type', iconType);
    b.appendChild(ic); return b;
  }
  if(on.seekBackward) group.insertBefore(makeSeek('-10','seek-backward-10'), anchor);
  if(on.seekForward) group.insertBefore(makeSeek('10','seek-forward-10'), anchor.nextSibling);
  seekInjected = true;
  return true;
}
function watchForSeekButtons(){
  if(injectSeekButtons()) return;
  var obs = new MutationObserver(function(){ if(injectSeekButtons()) obs.disconnect(); });
  obs.observe(document.body, { childList:true, subtree:true });
}

// Apply theme CSS variables (brand color, font, captions appearance). The
// default video layout defines its own --video-brand / --video-font-family on
// .vds-video-layout (defaulting to grey), which OVERRIDES the component-level
// --media-brand — so we must set BOTH, targeting the layout element with a
// higher-specificity rule injected after vidstack's stylesheet.
function applyTheme(){
  var cap = CFG.captions||{};
  var font = CFG.fontFamily ? '"'+CFG.fontFamily+'", sans-serif' : '';
  var v = '';
  if(CFG.primaryColor) v += '--media-brand:'+CFG.primaryColor+';--video-brand:'+CFG.primaryColor+';';
  if(font) v += '--media-font-family:'+font+';--video-font-family:'+font+';';
  if(cap.color) v += '--media-cue-color:'+cap.color+';';
  if(cap.background) v += '--media-cue-bg:'+cap.background+';';
  if(cap.fontSize) v += '--media-cue-font-size:'+cap.fontSize+'px;';
  if(!v) return;
  var st = document.createElement('style');
  st.textContent = 'media-player, media-player .vds-video-layout{'+v+'}';
  document.head.appendChild(st);
}

function beacon(event, extra){
  var payload = {
    videoId: VID, libraryId: LIB, sessionId: SID, visitorId: VISITOR, event: event,
    position: (player && player.currentTime) || 0
  };
  if(VT) payload.vt = VT;
  var body = JSON.stringify(Object.assign(payload, extra || {}));
  try {
    if (navigator.sendBeacon) navigator.sendBeacon("/beacon", new Blob([body], {type:"application/json"}));
    else fetch("/beacon", {method:"POST", body:body, headers:{"Content-Type":"application/json"}, keepalive:true});
  } catch(e){}
}

// token loader: re-append the master's ?exp&token to every child request.
function tokenQueryOf(u){ return (u.split('?')[1] || ''); }
function makeLoader(){
  var Base = window.Hls.DefaultConfig.loader;
  function L(){ Base.apply(this, arguments); }
  L.prototype = Object.create(Base.prototype);
  L.prototype.constructor = L;
  L.prototype.load = function(ctx, cfg, cbs){
    if (tokenQuery && ctx && ctx.url && ctx.url.indexOf('token=') === -1){
      ctx.url += (ctx.url.indexOf('?') === -1 ? '?' : '&') + tokenQuery;
    }
    Base.prototype.load.call(this, ctx, cfg, cbs);
  };
  return L;
}

function fetchPlay(){ return fetch(playURL, {cache:'no-store'}).then(function(r){
  if(!r.ok) throw new Error('play '+r.status); return r.json(); }); }

// 403/expired-token refresh: re-mint and reload at the same position.
var refreshing=false;
function refresh(){
  if(refreshing) return; refreshing=true;
  pendingSeek = player.currentTime || 0;
  fetchPlay().then(function(d){
    if(d.hlsUrl){ tokenQuery = tokenQueryOf(d.hlsUrl); setSrc(d.hlsUrl); }
  }).catch(function(){}).finally(function(){ refreshing=false; });
}

function setSrc(url){ player.src = { src: url, type: 'application/x-mpegurl' }; }
function setMp4Src(url){ player.src = { src: url, type: 'video/mp4' }; }

// Early-Play: while the original is playing, poll until the HLS master is ready
// and switch to it (preserving the current position).
function pollForReady(){
  var iv = setInterval(function(){
    fetchPlay().then(function(fresh){
      if(fresh.isReady && fresh.hlsUrl){
        clearInterval(iv);
        tokenQuery = tokenQueryOf(fresh.hlsUrl);
        pendingSeek = player.currentTime || 0;
        setSrc(fresh.hlsUrl);
      }
    }).catch(function(){});
  }, 5000);
}

// Bridge the token loader + error handling onto vidstack's HLS provider.
function setupHls(){
  player.addEventListener('provider-change', function(e){
    var provider = e.detail;
    if (provider && provider.type === 'hls' && window.Hls){
      provider.library = window.Hls;
      provider.config = { enableWorker:true, loader: makeLoader() };
    }
  });
  player.addEventListener('hls-error', function(e){
    var data = e.detail;
    if(data && data.response && data.response.code === 403){ refresh(); return; }
    if(data && data.fatal){
      beacon('error', {value:0});
      if(window.Hls && data.type === window.Hls.ErrorTypes.NETWORK_ERROR){ refresh(); }
    }
  });
}

function addTracks(d){
  (d.captions||[]).forEach(function(c){
    try { player.textTracks.add({ src:c.url, kind:'subtitles', label:c.label||c.lang, language:c.lang, type:'vtt' }); } catch(e){}
  });
  if(d.chaptersUrl){
    try { player.textTracks.add({ src:d.chaptersUrl, kind:'chapters', language:CFG.language||'en', type:'vtt', default:true }); } catch(e){}
  }
}

// Watchtime heatmap overlay above the time slider.
function renderHeatmap(){
  if(!CFG.showHeatmap) return;
  fetch(heatmapURL, {cache:'no-store'}).then(function(r){ return r.json(); }).then(function(j){
    var data = j && j.heatmap;
    if(!data || !data.length) return;
    var slider = player.el ? player.el.querySelector('media-time-slider') : document.querySelector('media-time-slider');
    if(!slider || !slider.parentElement) return;
    var host = slider.parentElement;
    if(getComputedStyle(host).position === 'static') host.style.position='relative';
    var bar = document.createElement('div'); bar.className='fz-heatmap';
    data.forEach(function(v){
      var s=document.createElement('span');
      s.style.height = Math.max(2, Math.round(v*100)) + '%';
      bar.appendChild(s);
    });
    host.appendChild(bar);
  }).catch(function(){});
}

// fmtTs renders seconds as m:ss for the search result chips.
function fmtTs(t){ t=Math.max(0,Math.round(t)); var m=Math.floor(t/60), s=t%60; return m+':'+(s<10?'0':'')+s; }

// In-player transcript search: a magnifier toggles a box; queries hit
// /embed/.../search; clicking a result seeks the player to that moment.
function setupSearch(){
  var wrap=document.getElementById('fz-search');
  var toggle=document.getElementById('fz-stoggle');
  var box=document.getElementById('fz-sbox');
  var input=document.getElementById('fz-sinput');
  var results=document.getElementById('fz-results');
  if(!wrap||!toggle||!box||!input||!results) return;
  wrap.style.display='block';
  toggle.addEventListener('click', function(){
    var open = box.style.display==='none';
    box.style.display = open ? 'block' : 'none';
    if(open) input.focus();
  });
  var timer=null;
  input.addEventListener('input', function(){
    var q=input.value.trim();
    if(timer) clearTimeout(timer);
    if(!q){ results.innerHTML=''; return; }
    timer=setTimeout(function(){
      fetch(searchURL+'?q='+encodeURIComponent(q), {cache:'no-store'})
        .then(function(r){ return r.json(); })
        .then(function(j){
          results.innerHTML='';
          (j.results||[]).forEach(function(h){
            var b=document.createElement('button');
            var ts=document.createElement('span'); ts.className='fz-ts'; ts.textContent=fmtTs(h.startSec);
            var tx=document.createElement('span'); tx.textContent=h.snippet;
            b.appendChild(ts); b.appendChild(tx);
            b.addEventListener('click', function(){
              try{ player.currentTime=h.startSec; player.play(); }catch(e){}
            });
            results.appendChild(b);
          });
        }).catch(function(){});
    }, 300);
  });
}

function wireEvents(){
  // Apply the default playback rate exactly once, on the first 'playing' — by
  // then vidstack's storage restore (which resets the rate to 1 during load) has
  // finished, so the value sticks; setting it earlier (on 'can-play') gets reset.
  // The flag means a viewer's later speed changes are never overridden.
  var rateApplied = false;
  function applyDefaultRate(){
    if(rateApplied) return; rateApplied = true;
    if(CFG.defaultSpeed && CFG.defaultSpeed !== 1){ try{ player.playbackRate = CFG.defaultSpeed; }catch(e){} }
  }
  player.addEventListener('can-play', function(){
    if(pendingSeek > 0){ try{ player.currentTime = pendingSeek; }catch(e){} pendingSeek = 0; }
    renderHeatmap();
    if(PREVIEW){ try{ player.muted = true; player.play(); }catch(e){} }
  });
  player.addEventListener('playing', function(){
    applyDefaultRate();
    if(!started){ started=true; beacon('start', {value: loadStart ? (performance.now()-loadStart) : 0}); }
  });
  player.addEventListener('waiting', function(){ if(started) beacon('rebuffer'); });
  player.addEventListener('ended', function(){ beacon('ended', {value:100}); try{ localStorage.removeItem(POSKEY); }catch(e){} });
  var lastTick = 0;
  player.addEventListener('time-update', function(){
    var now = performance.now();
    if(now - lastTick < 5000) return; lastTick = now;
    var pos = player.currentTime || 0;
    if(pos > 0){
      beacon('progress', {position: pos});
      if(CFG.resumePlayback){ try{ localStorage.setItem(POSKEY, String(pos)); }catch(e){} }
    }
  });
}

async function start(){
  applyControlVisibility();
  var d;
  try { d = await fetchPlay(); } catch(e){ msg.textContent = 'failed to load video'; return; }
  // Early-Play: if HLS isn't ready but the original is exposed, play it now and
  // switch to HLS once encoding finishes.
  var earlyPlay = !d.hlsUrl && d.earlyPlay && d.earlyPlayUrl;
  if(!d.hlsUrl && !earlyPlay){ msg.textContent = d.isReady===false ? 'video is still processing' : 'video unavailable'; return; }

  var layoutOpts = {};
  if(CFG.playbackSpeeds && CFG.playbackSpeeds.length) layoutOpts.playbackRates = CFG.playbackSpeeds;
  if(d.thumbnailsUrl) layoutOpts.thumbnails = d.thumbnailsUrl;
  if(CFG.language === 'tr') layoutOpts.translations = TR;

  // Create the player WITHOUT a source so the HLS loader is wired before load.
  // load:'eager' makes it load as soon as the page is visible (the default
  // 'visible' strategy uses an IntersectionObserver that can fail to fire inside
  // some iframe embeds). Media still defers while the tab is hidden (browser
  // policy), which is fine for an embed.
  player = await VidstackPlayer.create({
    target: '#target',
    poster: d.posterUrl || '',
    load: 'eager',
    posterLoad: 'eager',
    layout: new VidstackPlayerLayout(layoutOpts)
  });

  applyTheme();
  setupHls();
  wireEvents();
  addTracks(d);
  watchForSeekButtons();
  if(SHOW_SEARCH) setupSearch();

  if(CFG.resumePlayback){
    var resumed = false;
    if(progressURL){
      // Server-side resume (cross-device): a known viewer's saved position wins.
      try {
        var pj = await fetch(progressURL, {cache:'no-store'}).then(function(r){ return r.json(); });
        if(pj && pj.position > 0 && !pj.completed){ pendingSeek = pj.position; resumed = true; }
      } catch(e){}
    }
    if(!resumed){
      try{ var saved = parseFloat(localStorage.getItem(POSKEY)); if(saved>0) pendingSeek = saved; }catch(e){}
    }
  }

  // Deep link (?t=seconds) overrides resume — link straight to a moment.
  var deepT = parseFloat((/[?&]t=([0-9.]+)/.exec(location.search||'')||[])[1]);
  if(deepT > 0) pendingSeek = deepT;

  msg.style.display='none';
  loadStart = performance.now();
  if(d.hlsUrl){
    tokenQuery = tokenQueryOf(d.hlsUrl);
    setSrc(d.hlsUrl);
  } else {
    // Early-Play original (token carried on the URL query already).
    setMp4Src(d.earlyPlayUrl);
    pollForReady();
  }
}

start();
</script>
</body>
</html>`))
