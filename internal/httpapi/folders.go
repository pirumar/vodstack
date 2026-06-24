package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pirumar/vodstack/internal/db"
)

// folderRequest is the body for creating/updating a folder. ParentID/FolderID
// being a pointer lets a client send `null` to move to the root, distinct from
// omitting the field.
type folderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
}

type moveVideoRequest struct {
	FolderID *string `json:"folderId"`
}

// --- shared folder logic (library-scoped) ---

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request, libraryID string) {
	var req folderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	f, err := s.db.CreateFolder(r.Context(), uuid.NewString(), libraryID, req.ParentID, req.Name)
	if errors.Is(err, db.ErrInvalidFolder) {
		writeError(w, http.StatusBadRequest, "parent folder not found in this library")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) listFolders(w http.ResponseWriter, r *http.Request, libraryID string) {
	folders, err := s.db.ListFolders(r.Context(), libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

// updateFolder handles both rename (name set) and move (parentId present). A
// client moving to the root sends {"parentId": null}; renaming without moving
// omits parentId. We distinguish "omitted" from "null" by decoding into a map.
func (s *Server) updateFolder(w http.ResponseWriter, r *http.Request, libraryID, folderID string) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if nameRaw, ok := raw["name"]; ok {
		var name string
		if err := json.Unmarshal(nameRaw, &name); err == nil && strings.TrimSpace(name) != "" {
			if err := s.db.RenameFolder(r.Context(), libraryID, folderID, strings.TrimSpace(name)); errors.Is(err, db.ErrNotFound) {
				writeError(w, http.StatusNotFound, "folder not found")
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "rename failed")
				return
			}
		}
	}

	if parentRaw, ok := raw["parentId"]; ok {
		var parentID *string
		_ = json.Unmarshal(parentRaw, &parentID) // null -> nil (move to root)
		if err := s.db.MoveFolder(r.Context(), libraryID, folderID, parentID); errors.Is(err, db.ErrInvalidFolder) {
			writeError(w, http.StatusBadRequest, "invalid move (cycle or unknown parent)")
			return
		} else if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "move failed")
			return
		}
	}

	f, err := s.db.GetFolder(r.Context(), libraryID, folderID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request, libraryID, folderID string) {
	if err := s.db.DeleteFolder(r.Context(), libraryID, folderID); errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) moveVideo(w http.ResponseWriter, r *http.Request, libraryID, videoID string) {
	var req moveVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.db.MoveVideoToFolder(r.Context(), libraryID, videoID, req.FolderID); errors.Is(err, db.ErrInvalidFolder) {
		writeError(w, http.StatusBadRequest, "target folder not found in this library")
		return
	} else if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "video not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "move failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"videoId": videoID, "folderId": req.FolderID})
}

// --- admin BFF wrappers (operate on AdminLibraryID) ---

func (s *Server) adminListFolders(w http.ResponseWriter, r *http.Request) {
	s.listFolders(w, r, s.cfg.AdminLibraryID)
}
func (s *Server) adminCreateFolder(w http.ResponseWriter, r *http.Request) {
	s.createFolder(w, r, s.cfg.AdminLibraryID)
}
func (s *Server) adminUpdateFolder(w http.ResponseWriter, r *http.Request) {
	s.updateFolder(w, r, s.cfg.AdminLibraryID, chi.URLParam(r, "folderId"))
}
func (s *Server) adminDeleteFolder(w http.ResponseWriter, r *http.Request) {
	s.deleteFolder(w, r, s.cfg.AdminLibraryID, chi.URLParam(r, "folderId"))
}
func (s *Server) adminMoveVideo(w http.ResponseWriter, r *http.Request) {
	s.moveVideo(w, r, s.cfg.AdminLibraryID, chi.URLParam(r, "videoId"))
}

// --- library API wrappers (operate on the authenticated library) ---

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	s.listFolders(w, r, libraryFromCtx(r.Context()))
}
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	s.createFolder(w, r, libraryFromCtx(r.Context()))
}
func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	s.updateFolder(w, r, libraryFromCtx(r.Context()), chi.URLParam(r, "folderId"))
}
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	s.deleteFolder(w, r, libraryFromCtx(r.Context()), chi.URLParam(r, "folderId"))
}
func (s *Server) handleMoveVideo(w http.ResponseWriter, r *http.Request) {
	s.moveVideo(w, r, libraryFromCtx(r.Context()), chi.URLParam(r, "videoId"))
}
