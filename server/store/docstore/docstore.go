// Package docstore persists documents and their revisions in the plugin's own Postgres tables.
package docstore

import (
	"database/sql"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

var (
	// ErrNotFound is returned when a document or revision doesn't exist or was deleted.
	ErrNotFound = errors.New("not found")

	// ErrInvalidOrder is returned when an item to reorder isn't among the given folder's children.
	ErrInvalidOrder = errors.New("that item isn't in this folder")

	// ErrLocked is returned when another user holds the edit lock.
	ErrLocked = errors.New("document is locked by another user")

	// ErrVersionConflict is returned when a save is based on an outdated version.
	ErrVersionConflict = errors.New("document was changed since it was loaded")
)

// Doc is a Markdown document that belongs to a channel.
type Doc struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`

	// FolderID is the folder the document is in, or empty for the channel's top level.
	FolderID string `json:"folder_id"`

	// SortOrder is the manual position among the folder's documents; lower comes first.
	SortOrder int64 `json:"sort_order"`

	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	Version   int    `json:"version"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
	UpdatedBy string `json:"updated_by"`
	UpdatedAt int64  `json:"updated_at"`

	// LockUserID is the user currently editing, or empty. A lock past LockExpiresAt is free.
	LockUserID    string `json:"lock_user_id"`
	LockExpiresAt int64  `json:"lock_expires_at"`
}

// Revision is a saved version of a document.
type Revision struct {
	DocID     string `json:"doc_id"`
	Version   int    `json:"version"`
	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

// Store reads and writes documents. Locks live in the same row as the document so that
// acquiring one is a single conditional UPDATE, which is safe across a server cluster.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Store {
	return &Store{db: db, now: time.Now}
}

// Migrate creates the tables if needed. It is idempotent; callers must serialize it across
// the cluster because concurrent CREATE TABLE IF NOT EXISTS can still collide in Postgres.
func (s *Store) Migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS mmdocs_docs (
			id VARCHAR(26) PRIMARY KEY,
			channel_id VARCHAR(26) NOT NULL,
			title VARCHAR(256) NOT NULL,
			content TEXT NOT NULL,
			version INTEGER NOT NULL,
			created_by VARCHAR(26) NOT NULL,
			created_at BIGINT NOT NULL,
			updated_by VARCHAR(26) NOT NULL,
			updated_at BIGINT NOT NULL,
			deleted_at BIGINT NOT NULL DEFAULT 0,
			lock_user_id VARCHAR(26) NOT NULL DEFAULT '',
			lock_expires_at BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mmdocs_docs_channel ON mmdocs_docs (channel_id, deleted_at, updated_at)`,
		`CREATE TABLE IF NOT EXISTS mmdocs_revisions (
			doc_id VARCHAR(26) NOT NULL,
			version INTEGER NOT NULL,
			title VARCHAR(256) NOT NULL,
			content TEXT NOT NULL,
			created_by VARCHAR(26) NOT NULL,
			created_at BIGINT NOT NULL,
			PRIMARY KEY (doc_id, version)
		)`,
		// Added with folders; existing documents land at the top level.
		`ALTER TABLE mmdocs_docs ADD COLUMN IF NOT EXISTS folder_id VARCHAR(26) NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS mmdocs_folders (
			id VARCHAR(26) PRIMARY KEY,
			channel_id VARCHAR(26) NOT NULL,
			parent_id VARCHAR(26) NOT NULL,
			name VARCHAR(128) NOT NULL,
			created_by VARCHAR(26) NOT NULL,
			created_at BIGINT NOT NULL
		)`,
		// Sibling names are unique regardless of case, enforced by the database so concurrent
		// creates and renames can't both win.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mmdocs_folders_name ON mmdocs_folders (channel_id, parent_id, lower(name))`,
		// Added with manual ordering (0.4.0). The backfills number the rows that predate it,
		// in the order they were shown before: folders by name, documents by last update, per
		// parent scope. Like the webapp, a parent left by a deleted folder counts as the top
		// level: the LEFT JOIN misses it, so COALESCE gives those rows the empty parent. Later
		// rows start at 1, so they never return to 0 and aren't renumbered; after the first
		// run the UPDATEs match nothing.
		`ALTER TABLE mmdocs_docs ADD COLUMN IF NOT EXISTS sort_order BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE mmdocs_folders ADD COLUMN IF NOT EXISTS sort_order BIGINT NOT NULL DEFAULT 0`,
		`UPDATE mmdocs_docs d SET sort_order = n.rn
			FROM (SELECT m.id, row_number() OVER (PARTITION BY m.channel_id, COALESCE(f.id, '') ORDER BY m.updated_at DESC) AS rn
				FROM mmdocs_docs m LEFT JOIN mmdocs_folders f ON f.id = m.folder_id
				WHERE m.deleted_at = 0) n
			WHERE d.id = n.id AND d.sort_order = 0`,
		`UPDATE mmdocs_folders fd SET sort_order = n.rn
			FROM (SELECT m.id, row_number() OVER (PARTITION BY m.channel_id, COALESCE(p.id, '') ORDER BY lower(m.name)) AS rn
				FROM mmdocs_folders m LEFT JOIN mmdocs_folders p ON p.id = m.parent_id) n
			WHERE fd.id = n.id AND fd.sort_order = 0`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return errors.Wrap(err, "failed to run migration")
		}
	}
	return nil
}

const docColumns = `id, channel_id, folder_id, sort_order, title, content, version, created_by, created_at, updated_by, updated_at, lock_user_id, lock_expires_at`

func scanDoc(row interface{ Scan(...any) error }) (*Doc, error) {
	var d Doc
	err := row.Scan(&d.ID, &d.ChannelID, &d.FolderID, &d.SortOrder, &d.Title, &d.Content, &d.Version, &d.CreatedBy, &d.CreatedAt,
		&d.UpdatedBy, &d.UpdatedAt, &d.LockUserID, &d.LockExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan document")
	}
	return &d, nil
}

// Create inserts a new document and its first revision. folderID may be empty for the top level.
// The document joins the end of its folder's order.
func (s *Store) Create(channelID, folderID, userID, title, content string) (*Doc, error) {
	now := s.now().UnixMilli()
	d := &Doc{
		ID:        model.NewId(),
		ChannelID: channelID,
		FolderID:  folderID,
		Title:     title,
		Content:   content,
		Version:   1,
		CreatedBy: userID,
		CreatedAt: now,
		UpdatedBy: userID,
		UpdatedAt: now,
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	if err = tx.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM mmdocs_docs WHERE channel_id = $1 AND folder_id = $2 AND deleted_at = 0`,
		channelID, folderID).Scan(&d.SortOrder); err != nil {
		return nil, errors.Wrap(err, "failed to count sibling documents")
	}
	if _, err = tx.Exec(`INSERT INTO mmdocs_docs (`+docColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, '', 0)`,
		d.ID, d.ChannelID, d.FolderID, d.SortOrder, d.Title, d.Content, d.Version, d.CreatedBy, d.CreatedAt, d.UpdatedBy, d.UpdatedAt); err != nil {
		return nil, errors.Wrap(err, "failed to insert document")
	}
	if err = insertRevision(tx, d); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit document")
	}
	return d, nil
}

func insertRevision(tx *sql.Tx, d *Doc) error {
	_, err := tx.Exec(`INSERT INTO mmdocs_revisions (doc_id, version, title, content, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		d.ID, d.Version, d.Title, d.Content, d.UpdatedBy, d.UpdatedAt)
	return errors.Wrap(err, "failed to insert revision")
}

// Get returns a live document with its content.
func (s *Store) Get(docID string) (*Doc, error) {
	return scanDoc(s.db.QueryRow(`SELECT `+docColumns+` FROM mmdocs_docs WHERE id = $1 AND deleted_at = 0`, docID))
}

// ListByChannel returns a channel's live documents without content, in manual order (newest
// first as a tiebreak for any that somehow share a number).
func (s *Store) ListByChannel(channelID string) ([]*Doc, error) {
	rows, err := s.db.Query(`SELECT `+docColumns+` FROM mmdocs_docs WHERE channel_id = $1 AND deleted_at = 0 ORDER BY sort_order, updated_at DESC`, channelID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list documents")
	}
	defer func() { _ = rows.Close() }()

	docs := []*Doc{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		d.Content = ""
		docs = append(docs, d)
	}
	return docs, errors.Wrap(rows.Err(), "failed to iterate documents")
}

// Lock gives userID the edit lock until now+ttl, if it's free, expired, or already theirs.
// It returns the document as it stands afterwards; ErrLocked means someone else holds it.
func (s *Store) Lock(docID, userID string, ttl time.Duration) (*Doc, error) {
	now := s.now()
	res, err := s.db.Exec(`UPDATE mmdocs_docs SET lock_user_id = $1, lock_expires_at = $2
		WHERE id = $3 AND deleted_at = 0 AND (lock_user_id = '' OR lock_user_id = $1 OR lock_expires_at <= $4)`,
		userID, now.Add(ttl).UnixMilli(), docID, now.UnixMilli())
	if err != nil {
		return nil, errors.Wrap(err, "failed to lock document")
	}
	d, err := s.Get(docID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return d, ErrLocked
	}
	return d, nil
}

// Unlock releases userID's lock. Releasing a lock you don't hold is a no-op.
func (s *Store) Unlock(docID, userID string) error {
	_, err := s.db.Exec(`UPDATE mmdocs_docs SET lock_user_id = '', lock_expires_at = 0 WHERE id = $1 AND lock_user_id = $2`, docID, userID)
	return errors.Wrap(err, "failed to unlock document")
}

// Save writes a new version. The caller must hold an unexpired lock and must have based
// the edit on baseVersion; otherwise ErrLocked or ErrVersionConflict is returned.
func (s *Store) Save(docID, userID string, baseVersion int, title, content string) (*Doc, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	d, err := scanDoc(tx.QueryRow(`SELECT `+docColumns+` FROM mmdocs_docs WHERE id = $1 AND deleted_at = 0 FOR UPDATE`, docID))
	if err != nil {
		return nil, err
	}
	now := s.now()
	if d.LockUserID != userID || d.LockExpiresAt <= now.UnixMilli() {
		return nil, ErrLocked
	}
	if d.Version != baseVersion {
		return nil, ErrVersionConflict
	}
	if d.Title == title && d.Content == content {
		return d, nil
	}

	d.Title = title
	d.Content = content
	d.Version++
	d.UpdatedBy = userID
	d.UpdatedAt = now.UnixMilli()
	if _, err = tx.Exec(`UPDATE mmdocs_docs SET title = $1, content = $2, version = $3, updated_by = $4, updated_at = $5 WHERE id = $6`,
		d.Title, d.Content, d.Version, d.UpdatedBy, d.UpdatedAt, d.ID); err != nil {
		return nil, errors.Wrap(err, "failed to update document")
	}
	if err = insertRevision(tx, d); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit document")
	}
	return d, nil
}

// MoveDoc puts a document in folderID, or at the top level if folderID is empty. It doesn't need
// the edit lock and makes no revision: the folder is filing, not content. The document joins the
// end of its new folder's order.
func (s *Store) MoveDoc(docID, folderID string) error {
	var order int64
	err := s.db.QueryRow(`SELECT COALESCE((SELECT MAX(m.sort_order) + 1 FROM mmdocs_docs m
			WHERE m.channel_id = d.channel_id AND m.folder_id = $1 AND m.deleted_at = 0), 1)
		FROM mmdocs_docs d WHERE d.id = $2 AND d.deleted_at = 0`, folderID, docID).Scan(&order)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return errors.Wrap(err, "failed to count sibling documents")
	}
	res, err := s.db.Exec(`UPDATE mmdocs_docs SET folder_id = $1, sort_order = $2 WHERE id = $3 AND deleted_at = 0`,
		folderID, order, docID)
	if err != nil {
		return errors.Wrap(err, "failed to move document")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete soft-deletes a document; its revisions are kept.
func (s *Store) Delete(docID string) error {
	res, err := s.db.Exec(`UPDATE mmdocs_docs SET deleted_at = $1, lock_user_id = '', lock_expires_at = 0 WHERE id = $2 AND deleted_at = 0`,
		s.now().UnixMilli(), docID)
	if err != nil {
		return errors.Wrap(err, "failed to delete document")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRevisions returns a document's revisions without content, newest first.
func (s *Store) ListRevisions(docID string) ([]*Revision, error) {
	rows, err := s.db.Query(`SELECT doc_id, version, title, created_by, created_at FROM mmdocs_revisions WHERE doc_id = $1 ORDER BY version DESC`, docID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list revisions")
	}
	defer func() { _ = rows.Close() }()

	revs := []*Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.DocID, &r.Version, &r.Title, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, errors.Wrap(err, "failed to scan revision")
		}
		revs = append(revs, &r)
	}
	return revs, errors.Wrap(rows.Err(), "failed to iterate revisions")
}

// GetRevision returns one revision with its content.
func (s *Store) GetRevision(docID string, version int) (*Revision, error) {
	var r Revision
	err := s.db.QueryRow(`SELECT doc_id, version, title, content, created_by, created_at FROM mmdocs_revisions WHERE doc_id = $1 AND version = $2`, docID, version).
		Scan(&r.DocID, &r.Version, &r.Title, &r.Content, &r.CreatedBy, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get revision")
	}
	return &r, nil
}

// SetOrder numbers the given folders and documents 1..n in order among the children of parentID
// (empty for the top level) in channelID, as one transaction. Every ID must already belong to
// that parent; one whose parent was left by a deleted folder counts as a top-level child there,
// and is healed to parentID. The client sends each list in full; items not listed keep their
// numbers. No statement here may use one parameter both as a bare SET value and in a subquery
// comparison: the RPC database driver lets Postgres deduce parameter types, which fails with
// "inconsistent types deduced" when the roles disagree (see findings.md).
func (s *Store) SetOrder(channelID, parentID string, folderIDs, docIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	for i, id := range folderIDs {
		var res sql.Result
		if res, err = tx.Exec(`UPDATE mmdocs_folders SET sort_order = $1, parent_id = $2
			WHERE id = $3 AND channel_id = $4 AND (parent_id = $2
				OR ($2 = '' AND NOT EXISTS (SELECT 1 FROM mmdocs_folders p WHERE p.id = parent_id)))`,
			int64(i+1), parentID, id, channelID); err != nil {
			return errors.Wrap(err, "failed to set folder order")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrInvalidOrder
		}
	}
	for i, id := range docIDs {
		var res sql.Result
		if res, err = tx.Exec(`UPDATE mmdocs_docs SET sort_order = $1, folder_id = $2
			WHERE id = $3 AND channel_id = $4 AND deleted_at = 0 AND (folder_id = $2
				OR ($2 = '' AND NOT EXISTS (SELECT 1 FROM mmdocs_folders f WHERE f.id = folder_id)))`,
			int64(i+1), parentID, id, channelID); err != nil {
			return errors.Wrap(err, "failed to set document order")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrInvalidOrder
		}
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit order")
	}
	return nil
}
