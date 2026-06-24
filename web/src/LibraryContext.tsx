import { createContext, useContext } from 'react'

// App-wide session info, provided once the user is authenticated. Pages read it
// instead of threading props through the router.
export interface LibraryCtx {
  libraryId: string
  embedBaseUrl: string
  logout: () => void
}

const Ctx = createContext<LibraryCtx | null>(null)

export const LibraryProvider = Ctx.Provider

export function useLibrary(): LibraryCtx {
  const v = useContext(Ctx)
  if (!v) throw new Error('useLibrary must be used within LibraryProvider')
  return v
}
