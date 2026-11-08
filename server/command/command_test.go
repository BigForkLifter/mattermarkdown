package command

import (
	"errors"
	"testing"

	"github.com/BigForkLifter/mattermarkdown/server/store/docstore"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest/mock"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDocs struct {
	created   []string
	docs      []*docstore.Doc
	createErr error
}

func (f *fakeDocs) CreateDoc(_, _, title string) (*docstore.Doc, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, title)
	return &docstore.Doc{Title: title}, nil
}

func (f *fakeDocs) ListDocs(_, _ string) ([]*docstore.Doc, error) {
	return f.docs, nil
}

func setupTest(t *testing.T) (Command, *fakeDocs) {
	api := &plugintest.API{}
	api.On("RegisterCommand", mock.AnythingOfType("*model.Command")).Return(nil)
	t.Cleanup(func() { api.AssertExpectations(t) })

	docs := &fakeDocs{}
	return NewCommandHandler(pluginapi.NewClient(api, &plugintest.Driver{}), docs), docs
}

func TestDocNew(t *testing.T) {
	t.Run("creates with the rest of the line as title", func(t *testing.T) {
		cmd, docs := setupTest(t)
		resp, err := cmd.Handle(&model.CommandArgs{Command: "/doc new  Release   notes "})
		require.NoError(t, err)
		assert.Equal(t, []string{"Release notes"}, docs.created)
		assert.Empty(t, resp.Text, "the posted card is the response")
	})

	t.Run("requires a title", func(t *testing.T) {
		cmd, docs := setupTest(t)
		resp, err := cmd.Handle(&model.CommandArgs{Command: "/doc new"})
		require.NoError(t, err)
		assert.Empty(t, docs.created)
		assert.Equal(t, model.CommandResponseTypeEphemeral, resp.ResponseType)
	})

	t.Run("reports errors", func(t *testing.T) {
		cmd, docs := setupTest(t)
		docs.createErr = errors.New("nope")
		resp, err := cmd.Handle(&model.CommandArgs{Command: "/doc new X"})
		require.NoError(t, err)
		assert.Contains(t, resp.Text, "nope")
	})
}

func TestDocList(t *testing.T) {
	cmd, docs := setupTest(t)

	resp, err := cmd.Handle(&model.CommandArgs{Command: "/doc list"})
	require.NoError(t, err)
	assert.Contains(t, resp.Text, "no documents")

	docs.docs = []*docstore.Doc{{Title: "Runbook", Version: 3}}
	resp, err = cmd.Handle(&model.CommandArgs{Command: "/doc list"})
	require.NoError(t, err)
	assert.Contains(t, resp.Text, "**Runbook** (version 3)")
}

func TestDocHelp(t *testing.T) {
	cmd, _ := setupTest(t)
	for _, c := range []string{"/doc", "/doc wat"} {
		resp, err := cmd.Handle(&model.CommandArgs{Command: c})
		require.NoError(t, err)
		assert.Equal(t, helpText, resp.Text, c)
	}
}
