package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidFolder is returned when a folder operation would break the tree
// (e.g. moving a folder into its own subtree, or referencing a parent that
// belongs to another library).
var ErrInvalidFolder = errors.New("invalid folder operation")

// Folder is one node in a library's folder tree. ParentID is nil for a
// root-level folder.
type Folder struct {
	ID        string    `json:"id"`
	LibraryID string    `json:"libraryId"`
	ParentID  *string   `json:"parentId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateFolder inserts a new folder. If parentID is set it must belong to the
// same library, otherwise ErrInvalidFolder is returned.
func (d *DB) CreateFolder(ctx context.Context, id, libraryID string, parentID *string, name string) (*Folder, error) {
	if parentID != nil {
		ok, err := d.folderInLibrary(ctx, libraryID, *parentID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrInvalidFolder
		}
	}
	_, err := d.pool.Exec(ctx,
		`INSERT INTO folders (id, library_id, parent_id, name) VALUES ($1, $2, $3, $4)`,
		id, libraryID, parentID, name)
	if err != nil {
		return nil, err
	}
	return d.GetFolder(ctx, libraryID, id)
}

// GetFolder fetches a single folder scoped to a library.
func (d *DB) GetFolder(ctx context.Context, libraryID, id string) (*Folder, error) {
	var f Folder
	err := d.pool.QueryRow(ctx,
		`SELECT id, library_id, parent_id, name, created_at
		 FROM folders WHERE id=$1 AND library_id=$2`, id, libraryID,
	).Scan(&f.ID, &f.LibraryID, &f.ParentID, &f.Name, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFolders returns every folder in a library (the client assembles the tree
// from parent_id). Ordered by name for stable display.
func (d *DB) ListFolders(ctx context.Context, libraryID string) ([]Folder, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, library_id, parent_id, name, created_at
		 FROM folders WHERE library_id=$1 ORDER BY name`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Folder, 0)
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.ParentID, &f.Name, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RenameFolder updates a folder's name.
func (d *DB) RenameFolder(ctx context.Context, libraryID, id, name string) error {
	return d.exec(ctx,
		`UPDATE folders SET name=$3 WHERE id=$1 AND library_id=$2`, id, libraryID, name)
}

// MoveFolder reparents a folder. newParentID nil moves it to the root. It
// rejects moves that would create a cycle (a folder cannot become a descendant
// of itself) or that point at a parent in another library.
func (d *DB) MoveFolder(ctx context.Context, libraryID, id string, newParentID *string) error {
	if newParentID != nil {
		if *newParentID == id {
			return ErrInvalidFolder
		}
		ok, err := d.folderInLibrary(ctx, libraryID, *newParentID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidFolder
		}
		// Walk up from the proposed parent; if we reach id, the move is a cycle.
		cur := newParentID
		for cur != nil {
			if *cur == id {
				return ErrInvalidFolder
			}
			var next *string
			err := d.pool.QueryRow(ctx,
				`SELECT parent_id FROM folders WHERE id=$1 AND library_id=$2`, *cur, libraryID,
			).Scan(&next)
			if errors.Is(err, pgx.ErrNoRows) {
				break
			}
			if err != nil {
				return err
			}
			cur = next
		}
	}
	return d.exec(ctx,
		`UPDATE folders SET parent_id=$3 WHERE id=$1 AND library_id=$2`, id, libraryID, newParentID)
}

// DeleteFolder removes a folder. Its sub-folders cascade (FK ON DELETE CASCADE)
// and any videos inside fall back to the library root (FK ON DELETE SET NULL).
func (d *DB) DeleteFolder(ctx context.Context, libraryID, id string) error {
	return d.exec(ctx,
		`DELETE FROM folders WHERE id=$1 AND library_id=$2`, id, libraryID)
}

// MoveVideoToFolder assigns a video to a folder (nil -> library root). The
// target folder, if any, must be in the same library.
func (d *DB) MoveVideoToFolder(ctx context.Context, libraryID, videoID string, folderID *string) error {
	if folderID != nil {
		ok, err := d.folderInLibrary(ctx, libraryID, *folderID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidFolder
		}
	}
	return d.exec(ctx,
		`UPDATE videos SET folder_id=$3, updated_at=now()
		 WHERE id=$1 AND library_id=$2 AND deleted_at IS NULL`, videoID, libraryID, folderID)
}

// folderInLibrary reports whether a folder exists in the given library.
func (d *DB) folderInLibrary(ctx context.Context, libraryID, id string) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id=$1 AND library_id=$2)`, id, libraryID,
	).Scan(&exists)
	return exists, err
}
