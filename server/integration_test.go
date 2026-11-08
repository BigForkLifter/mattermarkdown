package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/BigForkLifter/mattermarkdown/server/store/docstore"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi/testhelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type apiClient struct {
	t       *testing.T
	baseURL string
	token   string
}

// do sends a request through the real server and decodes a JSON response into out, if given.
func (c *apiClient) do(method, path string, body any, out any) int {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(c.t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, c.baseURL+path, &buf)
	require.NoError(c.t, err)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(c.t, err)
	defer func() { _ = resp.Body.Close() }()
	if out != nil && resp.StatusCode != http.StatusNoContent {
		require.NoError(c.t, json.NewDecoder(resp.Body).Decode(out))
	}
	return resp.StatusCode
}

func newAPIClient(t *testing.T, th *testhelper.TestHelper, user *model.User) *apiClient {
	client := model.NewAPIv4Client(th.ServerURL)
	_, _, err := client.Login(t.Context(), user.Username, "Password1!")
	require.NoError(t, err)
	return &apiClient{t: t, baseURL: th.ServerURL + "/plugins/" + testhelper.PluginID() + "/api/v1", token: client.AuthToken}
}

// One Setup per test because each resets the server; the subtests share its state in order.
func TestDocumentLifecycle(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	bob := th.CreateUser()
	outsider := th.CreateUser()
	for _, u := range []*model.User{th.User, bob} {
		_, _, err := th.AdminClient.AddChannelMember(ctx, th.Channel.Id, u.Id)
		require.NoError(t, err)
	}
	alice := newAPIClient(t, th, th.User)
	bobAPI := newAPIClient(t, th, bob)
	outsiderAPI := newAPIClient(t, th, outsider)

	channelDocs := "/channels/" + th.Channel.Id + "/docs"
	var doc docstore.Doc

	t.Run("create posts a card", func(t *testing.T) {
		status := alice.do(http.MethodPost, channelDocs, map[string]string{"title": "  Runbook ", "content": "# Hi"}, &doc)
		require.Equal(t, http.StatusCreated, status)
		assert.Equal(t, "Runbook", doc.Title)
		assert.Equal(t, 1, doc.Version)

		posts, _, err := th.AdminClient.GetPostsForChannel(ctx, th.Channel.Id, 0, 10, "", false, false)
		require.NoError(t, err)
		var card *model.Post
		for _, p := range posts.Posts {
			if p.Type == docPostType {
				card = p
			}
		}
		require.NotNil(t, card, "expected a document card post")
		assert.Equal(t, doc.ID, card.GetProp("doc_id"))
		assert.Equal(t, th.User.Id, card.UserId)
	})

	t.Run("stores pasted Unicode line breaks as newlines", func(t *testing.T) {
		var pasted docstore.Doc
		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, channelDocs, map[string]string{"title": "Pasted", "content": "# A\u2028\u2028b\r\nc"}, &pasted))
		assert.Equal(t, "# A\n\nb\nc", pasted.Content)
		require.Equal(t, http.StatusNoContent, alice.do(http.MethodDelete, "/docs/"+pasted.ID, nil, nil))
	})

	t.Run("rejects a blank title", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, alice.do(http.MethodPost, channelDocs, map[string]string{"title": " "}, nil))
	})

	t.Run("non-members can't see the channel's documents", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, outsiderAPI.do(http.MethodGet, channelDocs, nil, nil))
		assert.Equal(t, http.StatusNotFound, outsiderAPI.do(http.MethodGet, "/docs/"+doc.ID, nil, nil))
		assert.Equal(t, http.StatusForbidden, outsiderAPI.do(http.MethodPost, channelDocs, map[string]string{"title": "x"}, nil))
	})

	t.Run("list omits content", func(t *testing.T) {
		var docs []docstore.Doc
		require.Equal(t, http.StatusOK, bobAPI.do(http.MethodGet, channelDocs, nil, &docs))
		require.Len(t, docs, 1)
		assert.Equal(t, doc.ID, docs[0].ID)
		assert.Empty(t, docs[0].Content)
	})

	t.Run("saving needs the lock", func(t *testing.T) {
		var errResp map[string]string
		status := alice.do(http.MethodPut, "/docs/"+doc.ID, saveDocRequest{BaseVersion: 1, Title: "Runbook", Content: "x"}, &errResp)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, "locked", errResp["id"])
	})

	t.Run("only one editor at a time", func(t *testing.T) {
		var locked docstore.Doc
		require.Equal(t, http.StatusOK, alice.do(http.MethodPost, "/docs/"+doc.ID+"/lock", nil, &locked))
		assert.Equal(t, th.User.Id, locked.LockUserID)

		// Renewing your own lock succeeds.
		require.Equal(t, http.StatusOK, alice.do(http.MethodPost, "/docs/"+doc.ID+"/lock", nil, nil))

		var held docstore.Doc
		require.Equal(t, http.StatusConflict, bobAPI.do(http.MethodPost, "/docs/"+doc.ID+"/lock", nil, &held))
		assert.Equal(t, th.User.Id, held.LockUserID, "the conflict says who holds the lock")

		// Bob can't release Alice's lock.
		require.Equal(t, http.StatusNoContent, bobAPI.do(http.MethodDelete, "/docs/"+doc.ID+"/lock", nil, nil))
		require.Equal(t, http.StatusConflict, bobAPI.do(http.MethodPost, "/docs/"+doc.ID+"/lock", nil, nil))
	})

	t.Run("save bumps the version and keeps a revision", func(t *testing.T) {
		var saved docstore.Doc
		require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+doc.ID, saveDocRequest{BaseVersion: 1, Title: "Runbook v2", Content: "# Hello"}, &saved))
		assert.Equal(t, 2, saved.Version)
		assert.Equal(t, "# Hello", saved.Content)

		// Saving unchanged content doesn't make an empty revision.
		require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+doc.ID, saveDocRequest{BaseVersion: 2, Title: "Runbook v2", Content: "# Hello"}, &saved))
		assert.Equal(t, 2, saved.Version)

		var errResp map[string]string
		require.Equal(t, http.StatusConflict, alice.do(http.MethodPut, "/docs/"+doc.ID, saveDocRequest{BaseVersion: 1, Title: "Stale", Content: "old"}, &errResp))
		assert.Equal(t, "version_conflict", errResp["id"])

		var revs []docstore.Revision
		require.Equal(t, http.StatusOK, bobAPI.do(http.MethodGet, "/docs/"+doc.ID+"/revisions", nil, &revs))
		require.Len(t, revs, 2)
		assert.Equal(t, 2, revs[0].Version)

		var first docstore.Revision
		require.Equal(t, http.StatusOK, bobAPI.do(http.MethodGet, "/docs/"+doc.ID+"/revisions/1", nil, &first))
		assert.Equal(t, "# Hi", first.Content)
		assert.Equal(t, "Runbook", first.Title)
	})

	t.Run("releasing the lock lets someone else edit", func(t *testing.T) {
		require.Equal(t, http.StatusNoContent, alice.do(http.MethodDelete, "/docs/"+doc.ID+"/lock", nil, nil))
		var got docstore.Doc
		require.Equal(t, http.StatusOK, bobAPI.do(http.MethodGet, "/docs/"+doc.ID, nil, &got))
		assert.Empty(t, got.LockUserID)
		require.Equal(t, http.StatusOK, bobAPI.do(http.MethodPost, "/docs/"+doc.ID+"/lock", nil, nil))
	})

	t.Run("only the creator or a channel admin can delete", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, bobAPI.do(http.MethodDelete, "/docs/"+doc.ID, nil, nil))
		require.Equal(t, http.StatusNoContent, alice.do(http.MethodDelete, "/docs/"+doc.ID, nil, nil))
		assert.Equal(t, http.StatusNotFound, alice.do(http.MethodGet, "/docs/"+doc.ID, nil, nil))
	})

	t.Run("saves stop at the hourly budget", func(t *testing.T) {
		var big docstore.Doc
		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, channelDocs, map[string]string{"title": "Big"}, &big))
		require.Equal(t, http.StatusOK, alice.do(http.MethodPost, "/docs/"+big.ID+"/lock", nil, nil))

		const chunk = 1_000_000 // under the 1 MiB request limit with the JSON around it
		version := big.Version
		for i := range saveBudgetBytes / chunk {
			content := fmt.Sprintf("%d %s", i, strings.Repeat("a", chunk-10))
			var saved docstore.Doc
			require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+big.ID, saveDocRequest{BaseVersion: version, Title: "Big", Content: content}, &saved), "save %d", i)
			version = saved.Version
		}

		var errResp map[string]string
		require.Equal(t, http.StatusTooManyRequests, alice.do(http.MethodPut, "/docs/"+big.ID, saveDocRequest{BaseVersion: version, Title: "Big", Content: strings.Repeat("b", chunk)}, &errResp))
		assert.Equal(t, "save_budget", errResp["id"])
		assert.Equal(t, http.StatusTooManyRequests, alice.do(http.MethodPost, channelDocs, map[string]string{"title": "Another", "content": strings.Repeat("c", chunk)}, nil),
			"new documents count against the same budget")

		// Small saves still fit, and other people have their own budget.
		require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+big.ID, saveDocRequest{BaseVersion: version, Title: "Big", Content: "short"}, nil))
		assert.Equal(t, http.StatusCreated, bobAPI.do(http.MethodPost, channelDocs, map[string]string{"title": "Bob's", "content": strings.Repeat("d", chunk)}, nil))
	})
}

func TestFolders(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	_, _, err := th.AdminClient.AddChannelMember(ctx, th.Channel.Id, th.User.Id)
	require.NoError(t, err)
	other := th.CreateChannel(model.ChannelTypeOpen)
	_, _, err = th.AdminClient.AddChannelMember(ctx, other.Id, th.User.Id)
	require.NoError(t, err)

	alice := newAPIClient(t, th, th.User)
	outsider := newAPIClient(t, th, th.CreateUser())
	folders := "/channels/" + th.Channel.Id + "/folders"

	var runbooks, oncall, elsewhere docstore.Folder
	var doc docstore.Doc

	t.Run("create and nest", func(t *testing.T) {
		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, folders, map[string]string{"name": " Runbooks "}, &runbooks))
		assert.Equal(t, "Runbooks", runbooks.Name)
		assert.Empty(t, runbooks.ParentID)

		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, folders, map[string]string{"name": "On-call", "parent_id": runbooks.ID}, &oncall))
		assert.Equal(t, runbooks.ID, oncall.ParentID)

		// Same name under a different parent is fine.
		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, folders, map[string]string{"name": "On-call"}, nil))

		var list []docstore.Folder
		require.Equal(t, http.StatusOK, alice.do(http.MethodGet, folders, nil, &list))
		assert.Len(t, list, 3)
	})

	t.Run("sibling names are unique ignoring case", func(t *testing.T) {
		var errResp map[string]string
		require.Equal(t, http.StatusConflict, alice.do(http.MethodPost, folders, map[string]string{"name": "RUNBOOKS"}, &errResp))
		assert.Equal(t, "folder_name_taken", errResp["id"])
	})

	t.Run("documents go in folders of their own channel only", func(t *testing.T) {
		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, "/channels/"+other.Id+"/folders", map[string]string{"name": "Elsewhere"}, &elsewhere))
		assert.Equal(t, http.StatusBadRequest, alice.do(http.MethodPost, "/channels/"+th.Channel.Id+"/docs",
			map[string]string{"title": "x", "folder_id": elsewhere.ID}, nil))
		assert.Equal(t, http.StatusBadRequest, alice.do(http.MethodPost, folders, map[string]string{"name": "x", "parent_id": elsewhere.ID}, nil))

		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, "/channels/"+th.Channel.Id+"/docs",
			map[string]string{"title": "Pager duty", "folder_id": oncall.ID}, &doc))
		assert.Equal(t, oncall.ID, doc.FolderID)
	})

	t.Run("no folder inside itself or its descendants", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, alice.do(http.MethodPatch, "/folders/"+runbooks.ID, map[string]string{"parent_id": oncall.ID}, nil))
		assert.Equal(t, http.StatusBadRequest, alice.do(http.MethodPatch, "/folders/"+runbooks.ID, map[string]string{"parent_id": runbooks.ID}, nil))
	})

	t.Run("rename and move", func(t *testing.T) {
		var renamed docstore.Folder
		require.Equal(t, http.StatusOK, alice.do(http.MethodPatch, "/folders/"+oncall.ID, map[string]string{"name": "Pager"}, &renamed))
		assert.Equal(t, "Pager", renamed.Name)
		assert.Equal(t, runbooks.ID, renamed.ParentID, "renaming keeps the parent")

		require.Equal(t, http.StatusOK, alice.do(http.MethodPatch, "/folders/"+oncall.ID, map[string]string{"parent_id": ""}, &renamed))
		assert.Empty(t, renamed.ParentID)
		assert.Equal(t, "Pager", renamed.Name, "moving keeps the name")
	})

	t.Run("only empty folders can be deleted", func(t *testing.T) {
		var errResp map[string]string
		require.Equal(t, http.StatusConflict, alice.do(http.MethodDelete, "/folders/"+oncall.ID, nil, &errResp))
		assert.Equal(t, "folder_not_empty", errResp["id"])

		var moved docstore.Doc
		require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+doc.ID+"/folder", map[string]string{"folder_id": ""}, &moved))
		assert.Empty(t, moved.FolderID)

		require.Equal(t, http.StatusNoContent, alice.do(http.MethodDelete, "/folders/"+oncall.ID, nil, nil))
		assert.Equal(t, http.StatusNotFound, alice.do(http.MethodGet, "/folders/"+oncall.ID, nil, nil))
	})

	t.Run("outsiders can't see folders", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, outsider.do(http.MethodGet, folders, nil, nil))
		assert.Equal(t, http.StatusNotFound, outsider.do(http.MethodGet, "/folders/"+runbooks.ID, nil, nil))
		assert.Equal(t, http.StatusNotFound, outsider.do(http.MethodPatch, "/folders/"+runbooks.ID, map[string]string{"name": "x"}, nil))
	})

	t.Run("a folder resolves to its channel for links", func(t *testing.T) {
		var got docstore.Folder
		require.Equal(t, http.StatusOK, alice.do(http.MethodGet, "/folders/"+runbooks.ID, nil, &got))
		assert.Equal(t, th.Channel.Id, got.ChannelID)
	})
}

// Reordering is a separate Setup so its titles start from a known order.
func TestOrdering(t *testing.T) {
	th := testhelper.Setup(t)
	ctx := t.Context()

	_, _, err := th.AdminClient.AddChannelMember(ctx, th.Channel.Id, th.User.Id)
	require.NoError(t, err)
	other := th.CreateChannel(model.ChannelTypeOpen)
	_, _, err = th.AdminClient.AddChannelMember(ctx, other.Id, th.User.Id)
	require.NoError(t, err)

	alice := newAPIClient(t, th, th.User)
	outsider := newAPIClient(t, th, th.CreateUser())
	orderPath := "/channels/" + th.Channel.Id + "/order"
	foldersPath := "/channels/" + th.Channel.Id + "/folders"
	docsPath := "/channels/" + th.Channel.Id + "/docs"

	var folder docstore.Folder
	require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, foldersPath, map[string]string{"name": "Zeta"}, &folder))
	require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, docsPath, map[string]string{"title": "First"}, nil))
	require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, docsPath, map[string]string{"title": "Second"}, nil))
	require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, docsPath, map[string]string{"title": "Third"}, nil))
	require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, docsPath, map[string]string{"title": "Filed", "folder_id": folder.ID}, nil))

	docIDs := func() map[string]string {
		var docs []docstore.Doc
		require.Equal(t, http.StatusOK, alice.do(http.MethodGet, docsPath, nil, &docs))
		ids := make(map[string]string, len(docs))
		for _, d := range docs {
			ids[d.Title] = d.ID
		}
		return ids
	}
	topLevelTitles := func() []string {
		var docs []docstore.Doc
		require.Equal(t, http.StatusOK, alice.do(http.MethodGet, docsPath, nil, &docs))
		titles := make([]string, 0, 3)
		for _, d := range docs {
			if d.FolderID == "" {
				titles = append(titles, d.Title)
			}
		}
		return titles
	}

	t.Run("new documents start at the end in creation order", func(t *testing.T) {
		assert.Equal(t, []string{"First", "Second", "Third"}, topLevelTitles())
	})

	t.Run("drag reorder stores a manual order", func(t *testing.T) {
		ids := docIDs()
		require.Equal(t, http.StatusNoContent, alice.do(http.MethodPut, orderPath,
			map[string]any{"parent_id": "", "folder_ids": []string{}, "doc_ids": []string{ids["Third"], ids["First"], ids["Second"]}}, nil))
		assert.Equal(t, []string{"Third", "First", "Second"}, topLevelTitles())

		// Moving out and back appends to the end rather than keeping the old slot.
		require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+ids["Third"]+"/folder", map[string]string{"folder_id": folder.ID}, nil))
		require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+ids["Third"]+"/folder", map[string]string{"folder_id": ""}, nil))
		assert.Equal(t, []string{"First", "Second", "Third"}, topLevelTitles())
	})

	t.Run("moving to the current folder changes nothing", func(t *testing.T) {
		ids := docIDs()
		require.Equal(t, http.StatusOK, alice.do(http.MethodPut, "/docs/"+ids["First"]+"/folder", map[string]string{"folder_id": ""}, nil))
		assert.Equal(t, []string{"First", "Second", "Third"}, topLevelTitles())
	})

	t.Run("reorder is scoped to one folder", func(t *testing.T) {
		filed := docIDs()["Filed"]
		require.Equal(t, http.StatusBadRequest, alice.do(http.MethodPut, orderPath,
			map[string]any{"parent_id": "", "folder_ids": []string{}, "doc_ids": []string{filed}}, nil))

		require.Equal(t, http.StatusNoContent, alice.do(http.MethodPut, orderPath,
			map[string]any{"parent_id": folder.ID, "folder_ids": []string{}, "doc_ids": []string{filed}}, nil))
	})

	t.Run("folders reorder among their siblings", func(t *testing.T) {
		var folder2 docstore.Folder
		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, foldersPath, map[string]string{"name": "Alpha"}, &folder2))

		require.Equal(t, http.StatusNoContent, alice.do(http.MethodPut, orderPath,
			map[string]any{"parent_id": "", "folder_ids": []string{folder2.ID, folder.ID}, "doc_ids": []string{}}, nil))

		var list []docstore.Folder
		require.Equal(t, http.StatusOK, alice.do(http.MethodGet, foldersPath, nil, &list))
		require.Len(t, list, 2)
		assert.Equal(t, []string{"Alpha", "Zeta"}, []string{list[0].Name, list[1].Name})
	})

	t.Run("only members may reorder", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, outsider.do(http.MethodPut, orderPath,
			map[string]any{"parent_id": "", "folder_ids": []string{}, "doc_ids": []string{}}, nil))

		// A parent folder from another channel is refused.
		var elsewhere docstore.Folder
		require.Equal(t, http.StatusCreated, alice.do(http.MethodPost, "/channels/"+other.Id+"/folders", map[string]string{"name": "Elsewhere"}, &elsewhere))
		assert.Equal(t, http.StatusBadRequest, alice.do(http.MethodPut, orderPath,
			map[string]any{"parent_id": elsewhere.ID, "folder_ids": []string{}, "doc_ids": []string{}}, nil))
	})
}
