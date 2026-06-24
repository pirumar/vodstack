import { useEffect, useRef, useState } from 'react'
import Hls from 'hls.js'
import { MediaPlayer, MediaProvider, Track, type MediaPlayerInstance } from '@vidstack/react'
import { defaultLayoutIcons, DefaultVideoLayout } from '@vidstack/react/player/layouts/default'
import '@vidstack/react/player/styles/default/theme.css'
import '@vidstack/react/player/styles/default/layouts/video.css'
import { api, type PlayData } from '../api'

// playSources picks the player sources: the HLS master (with an optional
// progressive MP4 fallback appended for clients without MSE/HLS), or — before
// encoding finishes — the Early-Play original. Returns null when nothing is
// playable yet.
function playSources(d: PlayData): { src: string; type: 'application/x-mpegurl' | 'video/mp4' }[] | null {
  if (d.hlsUrl) {
    const arr: { src: string; type: 'application/x-mpegurl' | 'video/mp4' }[] = [
      { src: d.hlsUrl, type: 'application/x-mpegurl' },
    ]
    if (d.mp4Url) arr.push({ src: d.mp4Url, type: 'video/mp4' })
    return arr
  }
  if (d.earlyPlay && d.earlyPlayUrl) {
    return [{ src: d.earlyPlayUrl, type: 'video/mp4' }]
  }
  return null
}

// Inline HLS player extracted from the former PlayerModal. Mints a signed play
// payload, re-appends the live token to every child request, and silently
// refreshes the token on a 403 mid-playback. Optionally seeks to a timestamp
// (driven by seekNonce so repeat clicks on the same moment still re-seek).
export function VideoPlayer({
  videoId,
  title,
  autoPlay = false,
  seekTo,
  seekNonce,
}: {
  videoId: string
  title: string
  autoPlay?: boolean
  seekTo?: number
  seekNonce?: number
}) {
  const [data, setData] = useState<PlayData | null>(null)
  const tokenQueryRef = useRef('')
  const playerRef = useRef<MediaPlayerInstance>(null)
  const pendingSeek = useRef<number | null>(null)

  // Apply an external seek request: jump now if the player can play, otherwise
  // stash it for the next can-play (covers seeks issued before/while loading).
  useEffect(() => {
    if (seekTo == null || seekNonce == null) return
    const p = playerRef.current
    if (p && p.state.canPlay) {
      p.currentTime = seekTo
      p.play().catch(() => {})
    } else {
      pendingSeek.current = seekTo
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seekNonce])

  useEffect(() => {
    setData(null)
    let cancelled = false
    api
      .play(videoId)
      .then((d) => {
        if (cancelled) return
        if (d.hlsUrl) tokenQueryRef.current = new URL(d.hlsUrl).search.replace(/^\?/, '')
        setData(d)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [videoId])

  // Early-Play: while the original is playing and HLS is not ready yet, poll for
  // completion and switch to the HLS master once it lands.
  useEffect(() => {
    if (!data || data.isReady || !data.earlyPlay) return
    const id = setInterval(() => {
      api
        .play(videoId)
        .then((fresh) => {
          if (fresh.isReady && fresh.hlsUrl) {
            tokenQueryRef.current = new URL(fresh.hlsUrl).search.replace(/^\?/, '')
            setData(fresh)
          }
        })
        .catch(() => {})
    }, 5000)
    return () => clearInterval(id)
  }, [data, videoId])

  function makeTokenLoader() {
    const Base = (Hls as unknown as { DefaultConfig: { loader: any } }).DefaultConfig.loader
    const ref = tokenQueryRef
    return class extends Base {
      load(context: any, config: any, cb: any) {
        const tq = ref.current
        if (tq && context?.url) {
          context.url = context.url.replace(/([?&])(exp|token)=[^&]*/g, '').replace(/[?&]$/, '')
          context.url += (context.url.includes('?') ? '&' : '?') + tq
        }
        super.load(context, config, cb)
      }
    }
  }

  // Preferred sources: HLS (with an optional progressive MP4 fallback for
  // HLS-less clients), or — before encoding finishes — the Early-Play original.
  const sources = data ? playSources(data) : null

  return (
    <div className="aspect-video overflow-hidden rounded-xl bg-black">
      {sources ? (
        <MediaPlayer
          ref={playerRef}
          className="h-full w-full"
          title={title}
          src={sources}
          poster={data?.posterUrl}
          crossOrigin
          playsInline
          autoPlay={autoPlay}
          onCanPlay={() => {
            if (pendingSeek.current != null) {
              const p = playerRef.current
              if (p) {
                p.currentTime = pendingSeek.current
                p.play().catch(() => {})
              }
              pendingSeek.current = null
            }
          }}
          onProviderChange={(provider: any) => {
            if (provider?.type === 'hls') {
              provider.library = Hls
              provider.config = { loader: makeTokenLoader() }
            }
          }}
          onProviderSetup={(provider: any) => {
            if (provider?.type !== 'hls' || !provider.instance) return
            let refreshing = false
            provider.instance.on(Hls.Events.ERROR, async (_e: any, d: any) => {
              if (d?.response?.code === 403 && !refreshing) {
                refreshing = true
                try {
                  const fresh = await api.play(videoId)
                  if (fresh.hlsUrl) {
                    tokenQueryRef.current = new URL(fresh.hlsUrl).search.replace(/^\?/, '')
                    provider.instance.startLoad()
                  }
                } catch {
                  /* ignore */
                } finally {
                  refreshing = false
                }
              }
            })
          }}
        >
          <MediaProvider />
          {data?.chaptersUrl && <Track kind="chapters" src={data.chaptersUrl} lang="tr" default />}
          {data?.captions?.map((c) => (
            <Track key={c.lang} kind="subtitles" src={c.url!} label={c.label} lang={c.lang} />
          ))}
          <DefaultVideoLayout thumbnails={data?.thumbnailsUrl} icons={defaultLayoutIcons} />
        </MediaPlayer>
      ) : (
        <div className="grid h-full place-items-center font-mono text-xs text-haze">yükleniyor…</div>
      )}
    </div>
  )
}
