package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/BigForkLifter/mattermarkdown/server/store/docstore"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

type createDocRequest struct {
	FolderID string `json:"folder_id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}

type moveDocRequest struct {
	FolderID string `json:"folder_id"`
}

type createFolderRequest struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
}

// updateFolderRequest leaves a field unchanged when it's absent.
type updateFolderRequest struct {
	ParentID *string `json:"parent_id"`
	Name     *string `json:"name"`
}

// setOrderRequest lists one folder view's children in their new display order.
type setOrderRequest struct {
	ParentID  string   `json:"parent_id"`
	FolderIDs []string `json:"folder_ids"`
	DocIDs    []string `json:"doc_ids"`
}

type saveDocRequest struct {
	BaseVersion int    `json:"base_version"`
	Title       string `json:"title"`
	Content     string `json:"content"`
}

func (p *Plugin) handleListDocs(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	channelID := mux.Vars(r)["channel_id"]
	if !p.canRead(userID, channelID) {
		writeError(w, r, errForbidden)
		return
	}
	docs, err := p.store.ListByChannel(channelID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for _, d := range docs {
		clearExpiredLock(d)
	}
	writeJSON(w, r, http.StatusOK, docs)
}

func (p *Plugin) handleCreateDoc(w http.ResponseWriter, r *http.Request) {
	var req createDocRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doc, err := p.createDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["channel_id"], req.FolderID, req.Title, req.Content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, doc)
}

func (p *Plugin) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	doc, err := p.getReadableDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"])
	if err != nil {
		writeError(w, r, err)
		return
	}
	clearExpiredLock(doc)
	writeJSON(w, r, http.StatusOK, doc)
}

func (p *Plugin) handleSaveDoc(w http.ResponseWriter, r *http.Request) {
	var req saveDocRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doc, err := p.saveDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"], req.BaseVersion, req.Title, req.Content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, doc)
}

func (p *Plugin) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	if err := p.deleteDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"]); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Plugin) handleLockDoc(w http.ResponseWriter, r *http.Request) {
	doc, err := p.lockDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"])
	if errors.Is(err, docstore.ErrLocked) {
		// The body names the current holder so the webapp can show who is editing.
		clearExpiredLock(doc)
		writeJSON(w, r, http.StatusConflict, doc)
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, doc)
}

func (p *Plugin) handleUnlockDoc(w http.ResponseWriter, r *http.Request) {
	if err := p.unlockDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"]); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Plugin) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	doc, err := p.getReadableDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"])
	if err != nil {
		writeError(w, r, err)
		return
	}
	revs, err := p.store.ListRevisions(doc.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, revs)
}

func (p *Plugin) handleGetRevision(w http.ResponseWriter, r *http.Request) {
	doc, err := p.getReadableDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"])
	if err != nil {
		writeError(w, r, err)
		return
	}
	version, err := strconv.Atoi(mux.Vars(r)["version"])
	if err != nil {
		writeError(w, r, docstore.ErrNotFound)
		return
	}
	rev, err := p.store.GetRevision(doc.ID, version)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, rev)
}

func (p *Plugin) handleMoveDoc(w http.ResponseWriter, r *http.Request) {
	var req moveDocRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doc, err := p.moveDoc(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["doc_id"], req.FolderID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	clearExpiredLock(doc)
	writeJSON(w, r, http.StatusOK, doc)
}

func (p *Plugin) handleListFolders(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	channelID := mux.Vars(r)["channel_id"]
	if !p.canRead(userID, channelID) {
		writeError(w, r, errForbidden)
		return
	}
	folders, err := p.store.ListFolders(channelID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, folders)
}

func (p *Plugin) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var req createFolderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	folder, err := p.createFolder(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["channel_id"], req.ParentID, req.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, folder)
}

func (p *Plugin) handleGetFolder(w http.ResponseWriter, r *http.Request) {
	folder, err := p.getReadableFolder(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["folder_id"])
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, folder)
}

func (p *Plugin) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	var req updateFolderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	folder, err := p.updateFolder(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["folder_id"], req.Name, req.ParentID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, folder)
}

func (p *Plugin) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := p.deleteFolder(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["folder_id"]); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Plugin) handleSetOrder(w http.ResponseWriter, r *http.Request) {
	var req setOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := p.setOrder(r.Header.Get("Mattermost-User-ID"), mux.Vars(r)["channel_id"], req.ParentID, req.FolderIDs, req.DocIDs); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearExpiredLock hides a lock that has lapsed, so clients don't compare against their own clocks.
func clearExpiredLock(doc *docstore.Doc) {
	if doc != nil && doc.LockExpiresAt <= time.Now().UnixMilli() {
		doc.LockUserID = ""
		doc.LockExpiresAt = 0
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeJSON(w, r, http.StatusRequestEntityTooLarge, map[string]string{"error": "document is too large"})
		return false
	}
	writeJSON(w, r, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	return false
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	id := ""
	switch {
	case errors.Is(err, docstore.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, errForbidden):
		status = http.StatusForbidden
	case errors.Is(err, errInvalidTitle), errors.Is(err, errInvalidFolderName), errors.Is(err, errInvalidFolder),
		errors.Is(err, errFolderCycle), errors.Is(err, docstore.ErrInvalidOrder):
		status = http.StatusBadRequest
	case errors.Is(err, docstore.ErrFolderNameTaken):
		status, id = http.StatusConflict, "folder_name_taken"
	case errors.Is(err, docstore.ErrFolderNotEmpty):
		status, id = http.StatusConflict, "folder_not_empty"
	case errors.Is(err, docstore.ErrLocked):
		status, id = http.StatusConflict, "locked"
	case errors.Is(err, docstore.ErrVersionConflict):
		status, id = http.StatusConflict, "version_conflict"
	case errors.Is(err, errSaveBudget):
		status, id = http.StatusTooManyRequests, "save_budget"
	}

	msg := err.Error()
	if status == http.StatusInternalServerError {
		requestLogger(r).WithError(err).Errorf("Request failed")
		msg = "internal server error"
	}
	writeJSON(w, r, status, map[string]string{"error": msg, "id": id})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		requestLogger(r).WithError(err).Errorf("Failed to write response")
	}
}
