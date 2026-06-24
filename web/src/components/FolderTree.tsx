import { useMemo, useState, type ReactNode } from 'react'
import {
  Folder as FolderIcon,
  FolderOpen,
  ChevronRight,
  Plus,
  Pencil,
  Trash2,
  Library,
  FolderTree as TreeIcon,
} from 'lucide-react'
import { api, type Folder } from '../api'

export type FolderSelection = 'all' | 'root' | string

export interface FolderCounts {
  all: number
  root: number
  byId: Record<string, number>
}

interface Props {
  folders: Folder[]
  selected: FolderSelection
  onSelect: (sel: FolderSelection) => void
  onChanged: () => void
  counts?: FolderCounts
  // Drop a video (by id) onto a folder. null targets the library root.
  onDropVideo?: (videoId: string, folderId: string | null) => void
}

// FolderTree is the library sidebar: pseudo-nodes for "all" and "root" plus the
// nested folder tree assembled from the flat parentId list. Folders show video
// counts and accept dropped video cards (drag-to-move).
export function FolderTree({ folders, selected, onSelect, onChanged, counts, onDropVideo }: Props) {
  const childrenOf = useMemo(() => {
    const map = new Map<string | null, Folder[]>()
    for (const f of folders) {
      const key = f.parentId ?? null
      const arr = map.get(key) ?? []
      arr.push(f)
      map.set(key, arr)
    }
    return map
  }, [folders])

  async function addRoot() {
    const name = window.prompt('Yeni klasör adı')?.trim()
    if (!name) return
    await api.createFolder(name, null).catch(() => {})
    onChanged()
  }

  return (
    <aside className="w-full shrink-0 sm:w-60">
      <div className="mb-3 flex items-center gap-2">
        <TreeIcon className="h-4 w-4 text-signal" />
        <h2 className="font-display text-sm font-semibold tracking-tight">Klasörler</h2>
        <button
          onClick={addRoot}
          className="ml-auto grid h-7 w-7 place-items-center rounded-md border border-edge text-haze transition hover:border-signal/40 hover:text-signal"
          aria-label="Yeni klasör"
          title="Yeni klasör"
        >
          <Plus className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="space-y-0.5">
        <Row
          icon={<Library className="h-3.5 w-3.5" />}
          label="Tüm videolar"
          active={selected === 'all'}
          count={counts?.all}
          onClick={() => onSelect('all')}
        />
        <Row
          icon={<FolderIcon className="h-3.5 w-3.5" />}
          label="Kök"
          active={selected === 'root'}
          count={counts?.root}
          onClick={() => onSelect('root')}
          onDropVideo={onDropVideo ? (id) => onDropVideo(id, null) : undefined}
        />
        {(childrenOf.get(null) ?? []).map((f) => (
          <FolderNode
            key={f.id}
            folder={f}
            depth={0}
            childrenOf={childrenOf}
            selected={selected}
            onSelect={onSelect}
            onChanged={onChanged}
            counts={counts}
            onDropVideo={onDropVideo}
          />
        ))}
      </div>
    </aside>
  )
}

function FolderNode({
  folder,
  depth,
  childrenOf,
  selected,
  onSelect,
  onChanged,
  counts,
  onDropVideo,
}: {
  folder: Folder
  depth: number
  childrenOf: Map<string | null, Folder[]>
  selected: FolderSelection
  onSelect: (sel: FolderSelection) => void
  onChanged: () => void
  counts?: FolderCounts
  onDropVideo?: (videoId: string, folderId: string | null) => void
}) {
  const [open, setOpen] = useState(true)
  const [over, setOver] = useState(false)
  const kids = childrenOf.get(folder.id) ?? []
  const active = selected === folder.id

  async function addSub() {
    const name = window.prompt(`"${folder.name}" içine yeni klasör`)?.trim()
    if (!name) return
    await api.createFolder(name, folder.id).catch(() => {})
    onChanged()
  }
  async function rename() {
    const name = window.prompt('Klasör adını değiştir', folder.name)?.trim()
    if (!name || name === folder.name) return
    await api.renameFolder(folder.id, name).catch(() => {})
    onChanged()
  }
  async function remove() {
    if (!window.confirm(`"${folder.name}" silinsin mi? Alt klasörler de silinir; içindeki videolar köke taşınır.`))
      return
    await api.deleteFolder(folder.id).catch(() => {})
    if (selected === folder.id) onSelect('all')
    onChanged()
  }

  const dropProps = onDropVideo
    ? {
        onDragOver: (e: React.DragEvent) => {
          e.preventDefault()
          setOver(true)
        },
        onDragLeave: () => setOver(false),
        onDrop: (e: React.DragEvent) => {
          e.preventDefault()
          setOver(false)
          const id = e.dataTransfer.getData('text/plain')
          if (id) onDropVideo(id, folder.id)
        },
      }
    : {}

  const count = counts?.byId[folder.id]

  return (
    <div>
      <div
        {...dropProps}
        className={`group flex items-center gap-1 rounded-lg pr-1 ring-1 ring-transparent transition ${
          over ? 'bg-signal/15 ring-signal/50' : active ? 'bg-edge text-chalk' : 'text-haze hover:bg-panel/60'
        }`}
        style={{ paddingLeft: depth * 12 }}
      >
        <button
          onClick={() => setOpen((o) => !o)}
          className={`grid h-6 w-5 place-items-center text-haze transition ${kids.length ? '' : 'opacity-0'}`}
          aria-label={open ? 'Daralt' : 'Genişlet'}
        >
          <ChevronRight className={`h-3.5 w-3.5 transition ${open ? 'rotate-90' : ''}`} />
        </button>
        <button
          onClick={() => onSelect(folder.id)}
          className="flex min-w-0 flex-1 items-center gap-1.5 py-1.5 text-left text-xs"
        >
          {active ? <FolderOpen className="h-3.5 w-3.5 shrink-0 text-signal" /> : <FolderIcon className="h-3.5 w-3.5 shrink-0" />}
          <span className="truncate">{folder.name}</span>
          {count !== undefined && count > 0 && <CountBadge n={count} />}
        </button>
        <div className="flex items-center opacity-0 transition group-hover:opacity-100">
          <IconBtn onClick={addSub} title="Alt klasör"><Plus className="h-3 w-3" /></IconBtn>
          <IconBtn onClick={rename} title="Yeniden adlandır"><Pencil className="h-3 w-3" /></IconBtn>
          <IconBtn onClick={remove} title="Sil" danger><Trash2 className="h-3 w-3" /></IconBtn>
        </div>
      </div>
      {open &&
        kids.map((k) => (
          <FolderNode
            key={k.id}
            folder={k}
            depth={depth + 1}
            childrenOf={childrenOf}
            selected={selected}
            onSelect={onSelect}
            onChanged={onChanged}
            counts={counts}
            onDropVideo={onDropVideo}
          />
        ))}
    </div>
  )
}

function Row({
  icon,
  label,
  active,
  count,
  onClick,
  onDropVideo,
}: {
  icon: ReactNode
  label: string
  active: boolean
  count?: number
  onClick: () => void
  onDropVideo?: (videoId: string) => void
}) {
  const [over, setOver] = useState(false)
  const dropProps = onDropVideo
    ? {
        onDragOver: (e: React.DragEvent) => {
          e.preventDefault()
          setOver(true)
        },
        onDragLeave: () => setOver(false),
        onDrop: (e: React.DragEvent) => {
          e.preventDefault()
          setOver(false)
          const id = e.dataTransfer.getData('text/plain')
          if (id) onDropVideo(id)
        },
      }
    : {}
  return (
    <button
      {...dropProps}
      onClick={onClick}
      className={`flex w-full items-center gap-1.5 rounded-lg py-1.5 pl-2 pr-2 text-left text-xs ring-1 ring-transparent transition ${
        over ? 'bg-signal/15 ring-signal/50' : active ? 'bg-edge text-chalk' : 'text-haze hover:bg-panel/60'
      }`}
    >
      {icon}
      <span className="truncate">{label}</span>
      {count !== undefined && count > 0 && <CountBadge n={count} />}
    </button>
  )
}

function CountBadge({ n }: { n: number }) {
  return (
    <span className="ml-auto rounded-full bg-ink/70 px-1.5 py-0.5 font-mono text-[9px] text-haze">{n}</span>
  )
}

function IconBtn({
  onClick,
  title,
  danger,
  children,
}: {
  onClick: () => void
  title: string
  danger?: boolean
  children: ReactNode
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      aria-label={title}
      className={`grid h-6 w-6 place-items-center rounded text-haze transition ${
        danger ? 'hover:text-bad' : 'hover:text-signal'
      }`}
    >
      {children}
    </button>
  )
}
