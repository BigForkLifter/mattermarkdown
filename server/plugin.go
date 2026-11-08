package main

import (
	"net/http"
	"sync"

	"github.com/BigForkLifter/mattermarkdown/server/command"
	"github.com/BigForkLifter/mattermarkdown/server/store/docstore"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/pkg/errors"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// store persists documents in the plugin's own database tables.
	store *docstore.Store

	// client is the Mattermost server API client.
	client *pluginapi.Client

	// commandClient is the client used to register and execute slash commands.
	commandClient command.Command

	// router is the HTTP router for handling API requests.
	router *mux.Router

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// saves limits how much document content each person can store per hour.
	saves saveBudget

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration
}

// OnActivate is invoked when the plugin is activated. If an error is returned, the plugin will be deactivated.
func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	if driver := p.client.Store.DriverName(); driver != model.DatabaseDriverPostgres {
		return errors.Errorf("unsupported database driver %q: MatterMarkdown requires Postgres", driver)
	}
	db, err := p.client.Store.GetMasterDB()
	if err != nil {
		return errors.Wrap(err, "failed to get database")
	}
	p.store = docstore.New(db)

	// Every node in a cluster activates the plugin; only one may migrate at a time.
	migrationMutex, err := cluster.NewMutex(p.API, "migrations")
	if err != nil {
		return errors.Wrap(err, "failed to create migration mutex")
	}
	migrationMutex.Lock()
	err = p.store.Migrate()
	migrationMutex.Unlock()
	if err != nil {
		return err
	}

	p.commandClient = command.NewCommandHandler(p.client, p)

	p.router = p.initRouter()

	return nil
}

// OnDeactivate is invoked when the plugin is deactivated.
func (p *Plugin) OnDeactivate() error {
	if p.client != nil {
		return p.client.Store.Close()
	}
	return nil
}

// This will execute the commands that were registered in the NewCommandHandler function.
func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	response, err := p.commandClient.Handle(args)
	if err != nil {
		return nil, model.NewAppError("ExecuteCommand", "plugin.command.execute_command.app_error", nil, err.Error(), http.StatusInternalServerError)
	}
	return response, nil
}

// CreateDoc and ListDocs let the command package act on documents with the same checks as the API.
func (p *Plugin) CreateDoc(userID, channelID, title string) (*docstore.Doc, error) {
	return p.createDoc(userID, channelID, "", title, "")
}

func (p *Plugin) ListDocs(userID, channelID string) ([]*docstore.Doc, error) {
	if !p.canRead(userID, channelID) {
		return nil, errForbidden
	}
	return p.store.ListByChannel(channelID)
}

// See https://developers.mattermost.com/extend/plugins/server/reference/
