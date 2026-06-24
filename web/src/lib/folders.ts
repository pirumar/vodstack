import { type Folder } from '../api'

// buildChildrenMap groups folders by their parentId (null = root level).
export function buildChildrenMap(folders: Folder[]): Map<string | null, Folder[]> {
  const map = new Map<string | null, Folder[]>()
  for (const f of folders) {
    const key = f.parentId ?? null
    const arr = map.get(key) ?? []
    arr.push(f)
    map.set(key, arr)
  }
  return map
}

export interface FlatFolder {
  folder: Folder
  depth: number
}

// flattenTree walks the folder forest depth-first, returning each folder with
// its nesting depth — used to render an indented picker/list from a flat list.
export function flattenTree(folders: Folder[]): FlatFolder[] {
  const map = buildChildrenMap(folders)
  const out: FlatFolder[] = []
  const walk = (parent: string | null, depth: number) => {
    for (const f of map.get(parent) ?? []) {
      out.push({ folder: f, depth })
      walk(f.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
}

// recursiveVideoCounts turns per-folder DIRECT video counts into per-folder
// SUBTREE totals (a folder's own videos plus everything in its descendants).
export function recursiveVideoCounts(
  folders: Folder[],
  directById: Record<string, number>,
): Record<string, number> {
  const map = buildChildrenMap(folders)
  const memo: Record<string, number> = {}
  const calc = (id: string): number => {
    if (memo[id] !== undefined) return memo[id]
    let total = directById[id] ?? 0
    for (const c of map.get(id) ?? []) total += calc(c.id)
    memo[id] = total
    return total
  }
  for (const f of folders) calc(f.id)
  return memo
}

// folderPath returns the ancestor chain from the root down to (and including)
// the given folder — used for breadcrumbs. Guards against cyclic parentId.
export function folderPath(folders: Folder[], id: string): Folder[] {
  const byId = new Map(folders.map((f) => [f.id, f]))
  const path: Folder[] = []
  const seen = new Set<string>()
  let cur = byId.get(id)
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id)
    path.unshift(cur)
    cur = cur.parentId ? byId.get(cur.parentId) : undefined
  }
  return path
}
