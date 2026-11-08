package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BigForkLifter/mattermarkdown/server/store/docstore"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

const maxFolderNameLength = 128

var (
	errInvalidFolderName = errors.Errorf("folder names must be 1 to %d characters, without \"/\"", maxFolderNameLength)
	errInvalidFolder     = errors.New("that folder isn't in this channel")
	errFolderCycle       = errors.New("a folder can't be moved into itself or one of its subfolders")
)

func normalizeFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxFolderNameLength || strings.ContainsRune(name, '/') ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errInvalidFolderName
	}
	return name, nil
}

// checkFolderInChannel accepts "" (the top level) or a folder belonging to channelID.
func (p *Plugin) checkFolderInChannel(folderID, channelID string) error {
	if folderID == "" {
		return nil
	}
	folder, err := p.store.GetFolder(folderID)
	if errors.Is(err, docstore.ErrNotFound) {
		return errInvalidFolder
	}
	if err != nil {
		return err
	}
	if folder.ChannelID != channelID {
		return errInvalidFolder
	}
	return nil
}

// getReadableFolder hides folders in channels the user can't read, like getReadableDoc.
func (p *Plugin) getReadableFolder(userID, folderID string) (*docstore.Folder, error) {
	folder, err := p.store.GetFolder(folderID)
	if err != nil {
		return nil, err
	}
	if !p.canRead(userID, folder.ChannelID) {
		return nil, docstore.ErrNotFound
	}
	return folder, nil
}

func (p *Plugin) createFolder(userID, channelID, parentID, name string) (*docstore.Folder, error) {
	name, err := normalizeFolderName(name)
	if err != nil {
		return nil, err
	}
	if !p.canWrite(userID, channelID) {
		return nil, errForbidden
	}
	if err = p.checkFolderInChannel(parentID, channelID); err != nil {
		return nil, err
	}
	folder, err := p.store.CreateFolder(channelID, parentID, userID, name)
	if err != nil {
		return nil, err
	}
	p.publishFolderChanged(folder, "folder_created")
	return folder, nil
}

// updateFolder renames and/or moves a folder; nil leaves that field unchanged.
func (p *Plugin) updateFolder(userID, folderID string, name, parentID *string) (*docstore.Folder, error) {
	folder, err := p.getReadableFolder(userID, folderID)
	if err != nil {
		return nil, err
	}
	if !p.canWrite(userID, folder.ChannelID) {
		return nil, errForbidden
	}

	newName, newParent := folder.Name, folder.ParentID
	moved := false
	if name != nil {
		if newName, err = normalizeFolderName(*name); err != nil {
			return nil, err
		}
	}
	if parentID != nil {
		newParent = *parentID
		if err = p.checkFolderInChannel(newParent, folder.ChannelID); err != nil {
			return nil, err
		}
		if err = p.checkNoCycle(folder, newParent); err != nil {
			return nil, err
		}
		moved = newParent != folder.ParentID
	}

	if err = p.store.UpdateFolder(folderID, newParent, newName, moved); err != nil {
		return nil, err
	}
	updated, err := p.store.GetFolder(folderID)
	if err != nil {
		return nil, err
	}
	p.publishFolderChanged(updated, "folder_updated")
	return updated, nil
}

// checkNoCycle refuses to put a folder inside itself or any of its descendants.
func (p *Plugin) checkNoCycle(folder *docstore.Folder, newParentID string) error {
	folders, err := p.store.ListFolders(folder.ChannelID)
	if err != nil {
		return err
	}
	parentOf := make(map[string]string, len(folders))
	for _, f := range folders {
		parentOf[f.ID] = f.ParentID
	}
	// Walk up from the new parent; the bound guards against a cycle already in the data.
	for id, steps := newParentID, 0; id != "" && steps <= len(folders); id, steps = parentOf[id], steps+1 {
		if id == folder.ID {
			return errFolderCycle
		}
	}
	return nil
}

func (p *Plugin) deleteFolder(userID, folderID string) error {
	folder, err := p.getReadableFolder(userID, folderID)
	if err != nil {
		return err
	}
	if !p.canWrite(userID, folder.ChannelID) {
		return errForbidden
	}
	if err := p.store.DeleteFolder(folderID); err != nil {
		return err
	}
	p.publishFolderChanged(folder, "folder_deleted")
	return nil
}

func (p *Plugin) moveDoc(userID, docID, folderID string) (*docstore.Doc, error) {
	doc, err := p.getReadableDoc(userID, docID)
	if err != nil {
		return nil, err
	}
	if !p.canWrite(userID, doc.ChannelID) {
		return nil, errForbidden
	}
	if err := p.checkFolderInChannel(folderID, doc.ChannelID); err != nil {
		return nil, err
	}
	if folderID == doc.FolderID {
		return doc, nil // filing it where it already is would wrongly move it to the end
	}
	if err := p.store.MoveDoc(docID, folderID); err != nil {
		return nil, err
	}
	doc.FolderID = folderID
	p.publishDocChanged(doc, "moved")
	return doc, nil
}

// setOrder stores a drag-and-drop reorder of one folder view: the view's folders and documents,
// each as the full list in its new display order. Like moveDoc, filing needs no lock and makes
// no revision.
func (p *Plugin) setOrder(userID, channelID, parentID string, folderIDs, docIDs []string) error {
	if !p.canWrite(userID, channelID) {
		return errForbidden
	}
	if err := p.checkFolderInChannel(parentID, channelID); err != nil {
		return err
	}
	if err := p.store.SetOrder(channelID, parentID, folderIDs, docIDs); err != nil {
		return err
	}
	if len(folderIDs) > 0 {
		p.publishReordered(channelID, "folder_reordered")
	}
	if len(docIDs) > 0 {
		p.publishReordered(channelID, "reordered")
	}
	return nil
}

// publishFolderChanged uses the same event as documents, with no doc_id, so lists refresh.
func (p *Plugin) publishFolderChanged(folder *docstore.Folder, action string) {
	p.API.PublishWebSocketEvent(wsEventDocChanged, map[string]any{
		"doc_id":     "",
		"folder_id":  folder.ID,
		"channel_id": folder.ChannelID,
		"action":     action,
	}, &model.WebsocketBroadcast{ChannelId: folder.ChannelID})
}
