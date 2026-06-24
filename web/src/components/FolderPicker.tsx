import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { Folder as FolderIcon, FolderOpen, Check, Search, X, Home } from 'lucide-react'
import { type Folder } from '../api'
import { flattenTree } from '../lib/folders'

interface Props {
  folders: Folder[]
  currentFolderId: string | null
  videoTitle: string
  // Optional per-folder video counts (subtree totals) to show as badges.
  countById?: Record<string, number>
  onPick: (folderId: string | null) => void
  onClose: () => void
}

// FolderPicker is a modal for moving a video into a folder: a search box over
// the full (indented) folder tree, with the video's current folder highlighted.
// Replaces the cramped inline <select> so nested folders are unambiguous.
export function FolderPicker({ folders, currentFolderId, videoTitle, countById, onPick, onClose }: Props) {
  const [q, setQ] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const flat = useMemo(() => flattenTree(folders), [folders])

  useEffect(() => {
    inputRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const query = q.trim().toLowerCase()
  // When searching, flatten to a plain matching list (no indentation).
  const rows = query
    ? flat.filter((x) => x.folder.name.toLowerCase().includes(query)).map((x) => ({ folder: x.folder, depth: 0 }))
    : flat

  const pick = (id: string | null) => {
    onPick(id)
    onClose()
  }

  return (
    <AnimatePresence>
      <motion.div
        className="fixed inset-0 z-50 grid place-items-center bg-ink/70 p-4 backdrop-blur-sm"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        onClick={onClose}
      >
        <motion.div
          className="flex max-h-[80vh] w-full max-w-md flex-col overflow-hidden rounded-2xl border border-edge bg-panel shadow-deck"
          initial={{ opacity: 0, scale: 0.96, y: 8 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.96 }}
          transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center gap-2.5 border-b border-edge px-4 py-3">
            <span className="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-signal/15 text-signal">
              <FolderOpen className="h-4 w-4" />
            </span>
            <div className="min-w-0">
              <div className="font-display text-sm font-semibold tracking-tight">Klasöre taşı</div>
              <div className="truncate font-mono text-[11px] text-haze" title={videoTitle}>
                {videoTitle}
              </div>
            </div>
            <button
              onClick={onClose}
              className="ml-auto grid h-7 w-7 place-items-center rounded-md text-haze transition hover:bg-edge hover:text-chalk"
              aria-label="Kapat"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          <div className="border-b border-edge p-3">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-haze" />
              <input
                ref={inputRef}
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="Klasör ara…"
                className="input w-full pl-9"
              />
            </div>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {!query && (
              <PickRow
                icon={<Home className="h-3.5 w-3.5" />}
                name="Kök"
                depth={0}
                active={currentFolderId === null}
                onClick={() => pick(null)}
              />
            )}
            {rows.length === 0 ? (
              <p className="px-3 py-8 text-center font-mono text-[11px] text-haze">Eşleşen klasör yok.</p>
            ) : (
              rows.map(({ folder, depth }) => (
                <PickRow
                  key={folder.id}
                  icon={
                    folder.id === currentFolderId ? (
                      <FolderOpen className="h-3.5 w-3.5 text-signal" />
                    ) : (
                      <FolderIcon className="h-3.5 w-3.5" />
                    )
                  }
                  name={folder.name}
                  depth={depth}
                  active={folder.id === currentFolderId}
                  count={countById?.[folder.id]}
                  onClick={() => pick(folder.id)}
                />
              ))
            )}
          </div>
        </motion.div>
      </motion.div>
    </AnimatePresence>
  )
}

function PickRow({
  icon,
  name,
  depth,
  active,
  count,
  onClick,
}: {
  icon: ReactNode
  name: string
  depth: number
  active: boolean
  count?: number
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-2 rounded-lg py-2 pr-2.5 text-left text-xs transition ${
        active ? 'bg-signal/15 text-chalk' : 'text-haze hover:bg-edge/60 hover:text-chalk'
      }`}
      style={{ paddingLeft: 10 + depth * 16 }}
    >
      <span className="shrink-0">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{name}</span>
      {count !== undefined && count > 0 && (
        <span className="rounded-full bg-ink/70 px-1.5 py-0.5 font-mono text-[9px] text-haze">{count}</span>
      )}
      {active && <Check className="h-3.5 w-3.5 shrink-0 text-signal" />}
    </button>
  )
}
