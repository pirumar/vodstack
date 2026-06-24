import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { motion } from 'framer-motion'
import { Clapperboard, Search, X, SlidersHorizontal, Folder as FolderIcon, ChevronRight } from 'lucide-react'
import { api, type Video, type Folder } from '../api'
import { UploadZone } from '../components/UploadZone'
import { VideoCard } from '../components/VideoCard'
import { FolderTree, type FolderSelection, type FolderCounts } from '../components/FolderTree'
import { VideoDetailDrawer } from '../components/VideoDetailDrawer'
import { PageHeader, EmptyState } from '../components/ui'
import { buildChildrenMap, recursiveVideoCounts, folderPath } from '../lib/folders'

type StatusFilter = 'all' | 'ready' | 'working' | 'failed' | 'pending'
type SortKey = 'recent' | 'title' | 'duration' | 'size'

const STATUS_OPTS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'Tüm durumlar' },
  { value: 'ready', label: 'Yayında' },
  { value: 'working', label: 'İşleniyor' },
  { value: 'pending', label: 'Bekliyor' },
  { value: 'failed', label: 'Başarısız' },
]

const SORT_OPTS: { value: SortKey; label: string }[] = [
  { value: 'recent', label: 'En yeni' },
  { value: 'title', label: 'Başlık (A→Z)' },
  { value: 'duration', label: 'Süre (uzun→kısa)' },
  { value: 'size', label: 'Boyut (büyük→küçük)' },
]

export function LibraryPage() {
  const navigate = useNavigate()
  const { videoId } = useParams()
  const [searchParams] = useSearchParams()
  const deepLinkTime = searchParams.get('t')

  const [allVideos, setAllVideos] = useState<Video[]>([])
  const [folders, setFolders] = useState<Folder[]>([])
  const [folderSel, setFolderSel] = useState<FolderSelection>('all')
  const [loading, setLoading] = useState(true)
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [sort, setSort] = useState<SortKey>('recent')
  const timer = useRef<number | null>(null)

  const refresh = useCallback(async () => {
    try {
      setAllVideos(await api.listVideos()) // fetch all; filtered client-side
    } catch {
      /* keep previous on transient error */
    } finally {
      setLoading(false)
    }
  }, [])

  const refreshFolders = useCallback(async () => {
    try {
      setFolders(await api.listFolders())
    } catch {
      /* keep previous on transient error */
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    refreshFolders()
  }, [refreshFolders])

  // Poll while anything is still processing.
  const anyWorking = allVideos.some((v) => v.status >= 1 && v.status <= 3)
  useEffect(() => {
    if (timer.current) window.clearInterval(timer.current)
    timer.current = window.setInterval(refresh, anyWorking ? 2500 : 15000)
    return () => {
      if (timer.current) window.clearInterval(timer.current)
    }
  }, [anyWorking, refresh])

  async function handleMove(v: Video, folderId: string | null) {
    setAllVideos((vs) => vs.map((x) => (x.videoId === v.videoId ? { ...x, folderId } : x)))
    try {
      await api.moveVideo(v.videoId, folderId)
      refresh()
    } catch {
      refresh()
    }
  }

  // Drag-drop: a video card dropped onto a folder in the tree.
  async function handleDropVideo(id: string, folderId: string | null) {
    setAllVideos((vs) => vs.map((x) => (x.videoId === id ? { ...x, folderId } : x)))
    try {
      await api.moveVideo(id, folderId)
      refresh()
    } catch {
      refresh()
    }
  }

  async function handleDelete(v: Video) {
    if (!confirm(`"${v.title}" çöp kutusuna taşınsın mı? 15 gün içinde geri alabilirsin.`)) return
    setAllVideos((vs) => vs.filter((x) => x.videoId !== v.videoId))
    try {
      await api.deleteVideo(v.videoId)
    } catch {
      refresh()
    }
  }

  // Folder counts. byId is the SUBTREE total (a folder's own videos plus all
  // descendants') so the sidebar badges, picker, and subfolder tiles agree.
  const counts: FolderCounts = useMemo(() => {
    const direct: Record<string, number> = {}
    let root = 0
    for (const v of allVideos) {
      if (v.folderId) direct[v.folderId] = (direct[v.folderId] ?? 0) + 1
      else root += 1
    }
    return { all: allVideos.length, root, byId: recursiveVideoCounts(folders, direct) }
  }, [allVideos, folders])

  // Subfolders of the current view (root level for "Kök", direct children for a
  // selected folder; none for the flat "all" view) + breadcrumb path.
  const childrenMap = useMemo(() => buildChildrenMap(folders), [folders])
  const subfolders = useMemo(() => {
    if (folderSel === 'all') return []
    return childrenMap.get(folderSel === 'root' ? null : folderSel) ?? []
  }, [childrenMap, folderSel])
  const breadcrumb = useMemo(
    () =>
      typeof folderSel === 'string' && folderSel !== 'all' && folderSel !== 'root'
        ? folderPath(folders, folderSel)
        : [],
    [folders, folderSel],
  )

  // Folder + status + search + sort pipeline.
  const displayed = useMemo(() => {
    let list = allVideos
    if (folderSel === 'root') list = list.filter((v) => !v.folderId)
    else if (folderSel !== 'all') list = list.filter((v) => v.folderId === folderSel)

    if (status !== 'all') {
      list = list.filter((v) => {
        if (status === 'ready') return v.status === 4
        if (status === 'working') return v.status >= 1 && v.status <= 3
        if (status === 'failed') return v.status === 5
        if (status === 'pending') return v.status === 0
        return true
      })
    }

    const q = query.trim().toLowerCase()
    if (q) list = list.filter((v) => v.title.toLowerCase().includes(q))

    if (sort !== 'recent') {
      list = [...list].sort((a, b) => {
        if (sort === 'title') return a.title.localeCompare(b.title, 'tr')
        if (sort === 'duration') return (b.length ?? 0) - (a.length ?? 0)
        if (sort === 'size') return (b.storageSize ?? 0) - (a.storageSize ?? 0)
        return 0
      })
    }
    return list
  }, [allVideos, folderSel, status, query, sort])

  const uploadFolderId = folderSel === 'all' || folderSel === 'root' ? null : folderSel
  const currentFolderName = useMemo(
    () => (typeof folderSel === 'string' ? folders.find((f) => f.id === folderSel)?.name : undefined),
    [folderSel, folders],
  )
  const filtersActive = status !== 'all' || query.trim() !== '' || sort !== 'recent'
  const open = (v: Video) => navigate(`/library/${v.videoId}`)

  return (
    <div>
      <PageHeader
        eyebrow="kütüphane"
        title={folderSel === 'all' ? 'Tüm videolar' : folderSel === 'root' ? 'Kök' : currentFolderName ?? 'Klasör'}
        count={displayed.length}
        icon={<Clapperboard className="h-5 w-5" />}
      />

      <div className="flex flex-col gap-8 lg:flex-row">
        <FolderTree
          folders={folders}
          selected={folderSel}
          onSelect={setFolderSel}
          onChanged={refreshFolders}
          counts={counts}
          onDropVideo={handleDropVideo}
        />

        <div className="min-w-0 flex-1">
          <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.5 }}>
            <UploadZone onUploaded={refresh} folderId={uploadFolderId} />
          </motion.div>

          {/* Filter toolbar */}
          <div className="mt-8 flex flex-wrap items-center gap-2.5">
            <div className="relative min-w-0 flex-1 sm:max-w-xs">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-haze" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Başlığa göre ara…"
                className="input w-full pl-9 pr-8"
              />
              {query && (
                <button
                  onClick={() => setQuery('')}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-haze hover:text-chalk"
                  aria-label="Temizle"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              )}
            </div>

            <select value={status} onChange={(e) => setStatus(e.target.value as StatusFilter)} className="input">
              {STATUS_OPTS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>

            <div className="relative">
              <SlidersHorizontal className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-haze" />
              <select value={sort} onChange={(e) => setSort(e.target.value as SortKey)} className="input pl-9">
                {SORT_OPTS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>

            {filtersActive && (
              <button
                onClick={() => {
                  setQuery('')
                  setStatus('all')
                  setSort('recent')
                }}
                className="btn-ghost text-xs"
              >
                <X className="h-3.5 w-3.5" /> Filtreleri temizle
              </button>
            )}
          </div>

          {/* Breadcrumb + subfolder tiles (folder drill-down) */}
          {(breadcrumb.length > 0 || subfolders.length > 0) && (
            <div className="mt-6">
              {breadcrumb.length > 0 && (
                <nav className="mb-3 flex flex-wrap items-center gap-1 font-mono text-[11px] text-haze">
                  <button onClick={() => setFolderSel('root')} className="transition hover:text-chalk">
                    Kök
                  </button>
                  {breadcrumb.map((f, i) => (
                    <span key={f.id} className="flex items-center gap-1">
                      <ChevronRight className="h-3 w-3 opacity-60" />
                      {i === breadcrumb.length - 1 ? (
                        <span className="text-chalk">{f.name}</span>
                      ) : (
                        <button onClick={() => setFolderSel(f.id)} className="transition hover:text-chalk">
                          {f.name}
                        </button>
                      )}
                    </span>
                  ))}
                </nav>
              )}
              {subfolders.length > 0 && (
                <>
                  <div className="eyebrow mb-2">alt klasörler</div>
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4">
                    {subfolders.map((f) => (
                      <FolderTile
                        key={f.id}
                        folder={f}
                        videoCount={counts.byId[f.id] ?? 0}
                        subCount={(childrenMap.get(f.id) ?? []).length}
                        onOpen={() => setFolderSel(f.id)}
                        onDropVideo={handleDropVideo}
                      />
                    ))}
                  </div>
                </>
              )}
            </div>
          )}

          <div className="mt-6">
            {loading ? (
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 3xl:grid-cols-5">
                {Array.from({ length: 8 }).map((_, i) => (
                  <div key={i} className="aspect-[4/3] animate-pulse rounded-2xl border border-edge bg-panel/40" />
                ))}
              </div>
            ) : displayed.length === 0 ? (
              subfolders.length > 0 && !filtersActive ? (
                <p className="py-8 text-center font-mono text-[11px] text-haze">
                  Bu klasörde doğrudan video yok — yukarıdaki alt klasörlere göz at.
                </p>
              ) : (
                <EmptyState
                  title={filtersActive ? 'Eşleşen video yok' : 'Henüz video yok'}
                  hint={filtersActive ? 'filtreleri değiştir veya temizle' : 'yukarıdan ilk dersini yükle'}
                />
              )
            ) : (
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 3xl:grid-cols-5">
                {displayed.map((v, i) => (
                  <VideoCard
                    key={v.videoId}
                    video={v}
                    index={i}
                    folders={folders}
                    folderCounts={counts.byId}
                    onPlay={open}
                    onManage={open}
                    onDelete={handleDelete}
                    onMove={handleMove}
                  />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      {videoId && (
        <VideoDetailDrawer
          videoId={videoId}
          initialSeek={deepLinkTime ? Number(deepLinkTime) : undefined}
          onClose={() => navigate('/library')}
          onChanged={refresh}
        />
      )}
    </div>
  )
}

// FolderTile is a navigable subfolder card shown above the video grid. It shows
// the subtree video count (+ subfolder count) and accepts dropped video cards.
function FolderTile({
  folder,
  videoCount,
  subCount,
  onOpen,
  onDropVideo,
}: {
  folder: Folder
  videoCount: number
  subCount: number
  onOpen: () => void
  onDropVideo: (videoId: string, folderId: string | null) => void
}) {
  const [over, setOver] = useState(false)
  return (
    <button
      onClick={onOpen}
      onDragOver={(e) => {
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault()
        setOver(false)
        const id = e.dataTransfer.getData('text/plain')
        if (id) onDropVideo(id, folder.id)
      }}
      className={`group flex items-center gap-3 rounded-xl border p-3 text-left transition ${
        over ? 'border-signal/60 bg-signal/10' : 'border-edge bg-panel/60 hover:border-signal/40'
      }`}
    >
      <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-edge/60 text-signal transition group-hover:bg-signal/15">
        <FolderIcon className="h-5 w-5" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate font-display text-sm font-semibold tracking-tight" title={folder.name}>
          {folder.name}
        </span>
        <span className="block font-mono text-[10px] text-haze">
          {videoCount} video{subCount > 0 ? ` · ${subCount} klasör` : ''}
        </span>
      </span>
      <ChevronRight className="h-4 w-4 shrink-0 text-haze opacity-0 transition group-hover:opacity-100" />
    </button>
  )
}
