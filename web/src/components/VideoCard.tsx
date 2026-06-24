import { useState } from 'react'
import { motion } from 'framer-motion'
import { Play, Trash2, Clock, Layers, SlidersHorizontal, FolderInput } from 'lucide-react'
import { type Video, type Folder } from '../api'
import { StatusChip } from './StatusChip'
import { FolderPicker } from './FolderPicker'
import { formatBytes, formatDuration } from '../lib/format'

interface Props {
  video: Video
  index: number
  folders: Folder[]
  // Optional per-folder subtree video counts, shown as badges in the picker.
  folderCounts?: Record<string, number>
  onPlay: (v: Video) => void
  onManage: (v: Video) => void
  onDelete: (v: Video) => void
  onMove: (v: Video, folderId: string | null) => void
}

export function VideoCard({ video, index, folders, folderCounts, onPlay, onManage, onDelete, onMove }: Props) {
  const ready = video.status === 4
  const failed = video.status === 5
  const working = video.status >= 1 && video.status <= 3
  const [pickerOpen, setPickerOpen] = useState(false)
  const currentFolderName = video.folderId ? folders.find((f) => f.id === video.folderId)?.name ?? 'Klasör' : 'Kök'

  return (
    <motion.div
      layout
      initial={{ opacity: 0, y: 14 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, delay: Math.min(index * 0.04, 0.4), ease: [0.16, 1, 0.3, 1] }}
      draggable
      onDragStartCapture={(e) => {
        e.dataTransfer.setData('text/plain', video.videoId)
        e.dataTransfer.effectAllowed = 'move'
      }}
      title="Klasöre taşımak için sürükleyin"
      className="group cursor-grab overflow-hidden rounded-2xl border border-edge bg-panel/60 shadow-deck transition hover:border-signal/40 active:cursor-grabbing"
    >
      {/* Poster / preview */}
      <div className="relative aspect-video overflow-hidden bg-ink">
        {ready && video.posterUrl ? (
          <img
            src={video.posterUrl}
            alt={video.title}
            loading="lazy"
            className="h-full w-full object-cover opacity-90 transition duration-500 group-hover:scale-105 group-hover:opacity-100"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center">
            {working && (
              <div className="relative h-full w-full overflow-hidden">
                <div className="absolute inset-0 -translate-x-full animate-shimmer bg-gradient-to-r from-transparent via-white/5 to-transparent" />
                <div className="flex h-full flex-col items-center justify-center gap-3 px-8">
                  <span className="font-mono text-[11px] uppercase tracking-[0.2em] text-signal">
                    {video.status === 3
                      ? `transcoding ${video.encodeProgress}%`
                      : video.status === 2
                        ? 'hazırlanıyor…'
                        : 'yükleniyor…'}
                  </span>
                  {video.status === 3 && (
                    <div className="h-1 w-full max-w-[180px] overflow-hidden rounded-full bg-ink">
                      <div
                        className="h-full rounded-full bg-signal transition-all duration-700 ease-out"
                        style={{ width: `${Math.max(2, video.encodeProgress)}%` }}
                      />
                    </div>
                  )}
                </div>
              </div>
            )}
            {failed && (
              <span className="font-mono text-[11px] uppercase tracking-[0.2em] text-bad">
                başarısız
              </span>
            )}
            {video.status === 0 && (
              <span className="font-mono text-[11px] uppercase tracking-[0.2em] text-idle">
                yükleme bekliyor
              </span>
            )}
          </div>
        )}

        <div className="absolute left-3 top-3">
          <StatusChip status={video.status} />
        </div>

        {ready && (
          <button
            onClick={() => onPlay(video)}
            className="absolute inset-0 grid place-items-center bg-ink/0 transition group-hover:bg-ink/40"
          >
            <span className="grid h-12 w-12 translate-y-1 place-items-center rounded-full bg-signal text-ink opacity-0 shadow-glow transition group-hover:translate-y-0 group-hover:opacity-100">
              <Play className="ml-0.5 h-5 w-5 fill-ink" />
            </span>
          </button>
        )}

        {ready && video.length ? (
          <span className="absolute bottom-3 right-3 rounded-md bg-ink/80 px-1.5 py-0.5 font-mono text-[11px] text-chalk">
            {formatDuration(video.length)}
          </span>
        ) : null}
      </div>

      {/* Meta */}
      <div className="p-4">
        <h3 className="truncate font-display text-[15px] font-semibold tracking-tight" title={video.title}>
          {video.title}
        </h3>

        <div className="mt-2.5 flex items-center gap-3 font-mono text-[11px] text-haze">
          <span className="inline-flex items-center gap-1">
            <Layers className="h-3 w-3" />
            {video.availableResolutions || '—'}
          </span>
          <span className="inline-flex items-center gap-1">
            <Clock className="h-3 w-3" />
            {formatDuration(video.length)}
          </span>
          <span className="ml-auto">{formatBytes(video.storageSize)}</span>
        </div>

        {failed && video.errorMessage && (
          <p className="mt-2 line-clamp-2 font-mono text-[10px] text-bad/80">{video.errorMessage}</p>
        )}

        <div className="mt-3 flex items-center gap-2 border-t border-edge/70 pt-3">
          <button
            onClick={() => onPlay(video)}
            disabled={!ready}
            className="inline-flex flex-1 items-center justify-center gap-1.5 rounded-lg bg-edge/60 px-3 py-2 text-xs font-semibold transition hover:bg-edge disabled:cursor-not-allowed disabled:opacity-30"
          >
            <Play className="h-3.5 w-3.5" /> Oynat
          </button>
          <button
            onClick={() => onManage(video)}
            disabled={!ready}
            className="inline-flex items-center justify-center rounded-lg bg-edge/60 px-3 py-2 text-xs text-haze transition hover:bg-signal/15 hover:text-signal disabled:cursor-not-allowed disabled:opacity-30"
            aria-label="Bölüm & altyazı"
            title="Bölümler ve altyazılar"
          >
            <SlidersHorizontal className="h-3.5 w-3.5" />
          </button>
          <button
            onClick={() => setPickerOpen(true)}
            aria-label="Klasöre taşı"
            title={`Klasör: ${currentFolderName} — değiştirmek için tıkla`}
            className="inline-flex min-w-0 items-center gap-1.5 rounded-lg bg-edge/60 px-2.5 py-2 text-xs text-haze transition hover:bg-signal/15 hover:text-signal"
          >
            <FolderInput className="h-3.5 w-3.5 shrink-0" />
            <span className="max-w-[6.5rem] truncate">{currentFolderName}</span>
          </button>
          <button
            onClick={() => onDelete(video)}
            className="inline-flex items-center justify-center rounded-lg bg-edge/60 px-3 py-2 text-xs text-haze transition hover:bg-bad/20 hover:text-bad"
            aria-label="Çöp kutusuna taşı"
            title="Çöp kutusuna taşı"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {pickerOpen && (
        <FolderPicker
          folders={folders}
          currentFolderId={video.folderId ?? null}
          videoTitle={video.title}
          countById={folderCounts}
          onPick={(fid) => onMove(video, fid)}
          onClose={() => setPickerOpen(false)}
        />
      )}
    </motion.div>
  )
}
