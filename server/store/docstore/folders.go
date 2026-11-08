package docstore

import (
	"database/sql"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

var (
	// ErrFolderNameTaken is returned when a sibling folder already has the name, ignoring case.
	ErrFolderNameTaken = errors.New("a folder with that name already exists here")

	// ErrFolderNotEmpty is returned when deleting a folder that still holds documents or folders.
	ErrFolderNotEmpty = errors.New("the folder isn't empty")
)

// Folder groups a channel's documents. Folders nest; ParentID is empty at the top level.
type Folder struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	ParentID  string `json:"parent_id"`

	// SortOrder is the manual position among its sibling folders; lower comes first.
	SortOrder int64  `json:"sort_order"`
	Name      string `json:"name"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

const folderColumns = `id, channel_id, parent_id, sort_order, name, created_by, created_at`

func scanFolder(row interface{ Scan(...any) error }) (*Folder, error) {
	var f Folder
	err := row.Scan(&f.ID, &f.ChannelID, &f.ParentID, &f.SortOrder, &f.Name, &f.CreatedBy, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan folder")
	}
	return &f, nil
}

// isDuplicateFolderName reports whether Postgres refused a duplicate sibling name. The plugin's
// database driver proxies over RPC, so errors arrive as text rather than *pq.Error; match the index.
func isDuplicateFolderName(err error) bool {
	return err != nil && strings.Contains(err.Error(), "idx_mmdocs_folders_name")
}

// CreateFolder adds a folder under parentID, or at the top level if parentID is empty. The folder
// joins the end of its siblings' order.
func (s *Store) CreateFolder(channelID, parentID, userID, name string) (*Folder, error) {
	var order int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM mmdocs_folders WHERE channel_id = $1 AND parent_id = $2`,
		channelID, parentID).Scan(&order); err != nil {
		return nil, errors.Wrap(err, "failed to count sibling folders")
	}
	f := &Folder{
		ID:        model.NewId(),
		ChannelID: channelID,
		ParentID:  parentID,
		SortOrder: order,
		Name:      name,
		CreatedBy: userID,
		CreatedAt: s.now().UnixMilli(),
	}
	_, err := s.db.Exec(`INSERT INTO mmdocs_folders (`+folderColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		f.ID, f.ChannelID, f.ParentID, f.SortOrder, f.Name, f.CreatedBy, f.CreatedAt)
	if isDuplicateFolderName(err) {
		return nil, ErrFolderNameTaken
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to insert folder")
	}
	return f, nil
}

func (s *Store) GetFolder(folderID string) (*Folder, error) {
	return scanFolder(s.db.QueryRow(`SELECT `+folderColumns+` FROM mmdocs_folders WHERE id = $1`, folderID))
}

// ListFolders returns all of a channel's folders, flat, in manual order; callers build the tree
// from ParentID, which preserves each parent's child order.
func (s *Store) ListFolders(channelID string) ([]*Folder, error) {
	rows, err := s.db.Query(`SELECT `+folderColumns+` FROM mmdocs_folders WHERE channel_id = $1 ORDER BY sort_order, lower(name)`, channelID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list folders")
	}
	defer func() { _ = rows.Close() }()

	folders := []*Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, errors.Wrap(rows.Err(), "failed to iterate folders")
}

// UpdateFolder renames and/or moves a folder. The caller checks that the new parent is in the
// same channel and isn't the folder itself or one of its descendants. Moving puts the folder at
// the end of its new siblings' order; renaming keeps its position.
func (s *Store) UpdateFolder(folderID, parentID, name string, moved bool) error {
	var order int64
	if moved {
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM mmdocs_folders
			WHERE channel_id = (SELECT channel_id FROM mmdocs_folders WHERE id = $1) AND parent_id = $2`,
			folderID, parentID).Scan(&order); err != nil {
			return errors.Wrap(err, "failed to count sibling folders")
		}
	}
	var res sql.Result
	var err error
	if moved {
		res, err = s.db.Exec(`UPDATE mmdocs_folders SET parent_id = $1, name = $2, sort_order = $3 WHERE id = $4`,
			parentID, name, order, folderID)
	} else {
		res, err = s.db.Exec(`UPDATE mmdocs_folders SET parent_id = $1, name = $2 WHERE id = $3`, parentID, name, folderID)
	}
	if isDuplicateFolderName(err) {
		return ErrFolderNameTaken
	}
	if err != nil {
		return errors.Wrap(err, "failed to update folder")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFolder removes an empty folder. Deleted documents don't count, so a folder whose
// documents were all deleted can go; those documents keep the stale folder ID.
func (s *Store) DeleteFolder(folderID string) error {
	res, err := s.db.Exec(`DELETE FROM mmdocs_folders f WHERE f.id = $1
		AND NOT EXISTS (SELECT 1 FROM mmdocs_folders c WHERE c.parent_id = f.id)
		AND NOT EXISTS (SELECT 1 FROM mmdocs_docs d WHERE d.folder_id = f.id AND d.deleted_at = 0)`, folderID)
	if err != nil {
		return errors.Wrap(err, "failed to delete folder")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetFolder(folderID); err != nil {
			return err
		}
		return ErrFolderNotEmpty
	}
	return nil
}
