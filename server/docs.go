package main

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BigForkLifter/mattermarkdown/server/store/docstore"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

const (
	// lockTTL is how long an edit lock lasts without a heartbeat. The webapp renews it every 30s.
	lockTTL = 90 * time.Second

	maxTitleLength = 256

	// docPostType renders as a document card in the channel. Custom types must start with "custom_".
	docPostType = "custom_mmdocs_doc"

	// wsEventDocChanged reaches clients as "custom_<plugin id>_doc_changed".
	wsEventDocChanged = "doc_changed"
)

var (
	errForbidden    = errors.New("you don't have permission to do that in this channel")
	errInvalidTitle = errors.Errorf("title must be between 1 and %d characters", maxTitleLength)
)

func normalizeTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleLength {
		return "", errInvalidTitle
	}
	return title, nil
}

// lineBreaks turns the other line breaks that text copied from word processors, PDFs and web
// pages can carry into "\n"; Markdown only breaks lines on "\n". Keep in sync with
// webapp/src/text.ts. "\r\n" comes first so it becomes one break, not two.
var lineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\v", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n")

func normalizeLineBreaks(content string) string {
	return lineBreaks.Replace(content)
}

func (p *Plugin) canRead(userID, channelID string) bool {
	return p.API.HasPermissionToChannel(userID, channelID, model.PermissionReadChannel)
}

// canWrite requires permission to post, so read-only and archived channels can't be edited.
func (p *Plugin) canWrite(userID, channelID string) bool {
	if !p.API.HasPermissionToChannel(userID, channelID, model.PermissionCreatePost) {
		return false
	}
	channel, err := p.client.Channel.Get(channelID)
	return err == nil && channel.DeleteAt == 0
}

// canDelete lets the creator delete a document, and anyone who may delete others' posts
// (channel admins by default).
func (p *Plugin) canDelete(userID string, doc *docstore.Doc) bool {
	if doc.CreatedBy == userID {
		return p.canWrite(userID, doc.ChannelID)
	}
	return p.API.HasPermissionToChannel(userID, doc.ChannelID, model.PermissionDeleteOthersPosts)
}

// getReadableDoc returns docstore.ErrNotFound rather than errForbidden for documents in
// channels the user can't read, so document IDs don't reveal anything.
func (p *Plugin) getReadableDoc(userID, docID string) (*docstore.Doc, error) {
	doc, err := p.store.Get(docID)
	if err != nil {
		return nil, err
	}
	if !p.canRead(userID, doc.ChannelID) {
		return nil, docstore.ErrNotFound
	}
	return doc, nil
}

func (p *Plugin) createDoc(userID, channelID, folderID, title, content string) (*docstore.Doc, error) {
	title, err := normalizeTitle(title)
	if err != nil {
		return nil, err
	}
	if !p.canWrite(userID, channelID) {
		return nil, errForbidden
	}

	if err = p.checkFolderInChannel(folderID, channelID); err != nil {
		return nil, err
	}

	content = normalizeLineBreaks(content)
	size := int64(len(title) + len(content))
	if !p.saves.allow(userID, size) {
		return nil, errSaveBudget
	}
	doc, err := p.store.Create(channelID, folderID, userID, title, content)
	if err != nil {
		return nil, err
	}
	p.saves.add(userID, size)

	post := &model.Post{
		UserId:    userID,
		ChannelId: channelID,
		Type:      docPostType,
		Message:   "created the document **" + title + "**",
	}
	post.AddProp("doc_id", doc.ID)
	post.AddProp("doc_title", title)
	if err := p.client.Post.CreatePost(post); err != nil {
		p.API.LogWarn("Failed to post document card", "doc_id", doc.ID, "err", err.Error())
	}

	p.publishDocChanged(doc, "created")
	return doc, nil
}

func (p *Plugin) saveDoc(userID, docID string, baseVersion int, title, content string) (*docstore.Doc, error) {
	title, err := normalizeTitle(title)
	if err != nil {
		return nil, err
	}
	doc, err := p.getReadableDoc(userID, docID)
	if err != nil {
		return nil, err
	}
	if !p.canWrite(userID, doc.ChannelID) {
		return nil, errForbidden
	}

	content = normalizeLineBreaks(content)
	size := int64(len(title) + len(content))
	if !p.saves.allow(userID, size) {
		return nil, errSaveBudget
	}
	saved, err := p.store.Save(docID, userID, baseVersion, title, content)
	if err != nil {
		return nil, err
	}
	if saved.Version != doc.Version {
		p.saves.add(userID, size)
		p.publishDocChanged(saved, "updated")
	}
	return saved, nil
}

// lockDoc acquires or renews the edit lock. On docstore.ErrLocked it also returns the
// document so the caller can say who holds the lock.
func (p *Plugin) lockDoc(userID, docID string) (*docstore.Doc, error) {
	doc, err := p.getReadableDoc(userID, docID)
	if err != nil {
		return nil, err
	}
	if !p.canWrite(userID, doc.ChannelID) {
		return nil, errForbidden
	}

	wasHeld := doc.LockUserID == userID && doc.LockExpiresAt > time.Now().UnixMilli()
	locked, err := p.store.Lock(docID, userID, lockTTL)
	if err != nil {
		return locked, err
	}
	// Heartbeats renew the lock every 30s; only tell clients when the holder changes.
	if !wasHeld {
		p.publishDocChanged(locked, "locked")
	}
	return locked, nil
}

func (p *Plugin) unlockDoc(userID, docID string) error {
	doc, err := p.getReadableDoc(userID, docID)
	if err != nil {
		return err
	}
	if doc.LockUserID != userID {
		return nil
	}
	if err := p.store.Unlock(docID, userID); err != nil {
		return err
	}
	doc.LockUserID = ""
	doc.LockExpiresAt = 0
	p.publishDocChanged(doc, "unlocked")
	return nil
}

func (p *Plugin) deleteDoc(userID, docID string) error {
	doc, err := p.getReadableDoc(userID, docID)
	if err != nil {
		return err
	}
	if !p.canDelete(userID, doc) {
		return errForbidden
	}
	if err := p.store.Delete(docID); err != nil {
		return err
	}
	p.publishDocChanged(doc, "deleted")
	return nil
}

// publishDocChanged tells the channel's clients to refresh. It carries no content, so
// clients fetch through the API and its permission checks.
func (p *Plugin) publishDocChanged(doc *docstore.Doc, action string) {
	p.API.PublishWebSocketEvent(wsEventDocChanged, map[string]any{
		"doc_id":     doc.ID,
		"channel_id": doc.ChannelID,
		"action":     action,
	}, &model.WebsocketBroadcast{ChannelId: doc.ChannelID})
}

// publishReordered tells clients the channel's manual ordering changed. Like the folder events,
// it carries no document ID; clients refresh their lists.
func (p *Plugin) publishReordered(channelID, action string) {
	p.API.PublishWebSocketEvent(wsEventDocChanged, map[string]any{
		"doc_id":     "",
		"folder_id":  "",
		"channel_id": channelID,
		"action":     action,
	}, &model.WebsocketBroadcast{ChannelId: channelID})
}
