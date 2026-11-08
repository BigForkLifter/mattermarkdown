package command

import (
	"fmt"
	"strings"

	"github.com/BigForkLifter/mattermarkdown/server/store/docstore"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

// DocService is the part of the plugin that commands act through, so they get the same
// permission checks and notifications as the REST API.
type DocService interface {
	CreateDoc(userID, channelID, title string) (*docstore.Doc, error)
	ListDocs(userID, channelID string) ([]*docstore.Doc, error)
}

type Handler struct {
	client *pluginapi.Client
	docs   DocService
}

type Command interface {
	Handle(args *model.CommandArgs) (*model.CommandResponse, error)
}

const docCommandTrigger = "doc"

const helpText = "* `/doc new <title>` creates a Markdown document in this channel\n" +
	"* `/doc list` lists this channel's documents\n\n" +
	"Open documents from the **Docs** button in the channel header."

// Register all your slash commands in the NewCommandHandler function.
func NewCommandHandler(client *pluginapi.Client, docs DocService) Command {
	autocomplete := model.NewAutocompleteData(docCommandTrigger, "[command]", "Markdown documents for this channel")
	newCmd := model.NewAutocompleteData("new", "<title>", "Create a document in this channel")
	newCmd.AddTextArgument("Title of the document", "<title>", "")
	autocomplete.AddCommand(newCmd)
	autocomplete.AddCommand(model.NewAutocompleteData("list", "", "List this channel's documents"))

	err := client.SlashCommand.Register(&model.Command{
		Trigger:          docCommandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Markdown documents for this channel",
		AutoCompleteHint: "[new <title> | list]",
		AutocompleteData: autocomplete,
	})
	if err != nil {
		client.Log.Error("Failed to register command", "error", err)
	}
	return &Handler{
		client: client,
		docs:   docs,
	}
}

// ExecuteCommand hook calls this method to execute the commands that were registered in the NewCommandHandler function.
func (c *Handler) Handle(args *model.CommandArgs) (*model.CommandResponse, error) {
	fields := strings.Fields(args.Command)
	if len(fields) == 0 || strings.TrimPrefix(fields[0], "/") != docCommandTrigger {
		return ephemeral(fmt.Sprintf("Unknown command: %s", args.Command)), nil
	}
	if len(fields) < 2 {
		return ephemeral(helpText), nil
	}

	switch fields[1] {
	case "new":
		return c.executeNew(args, strings.Join(fields[2:], " ")), nil
	case "list":
		return c.executeList(args), nil
	default:
		return ephemeral(helpText), nil
	}
}

func (c *Handler) executeNew(args *model.CommandArgs, title string) *model.CommandResponse {
	if strings.TrimSpace(title) == "" {
		return ephemeral("Please give the document a title: `/doc new <title>`")
	}
	// On success the document card posted in the channel is the response.
	if _, err := c.docs.CreateDoc(args.UserId, args.ChannelId, title); err != nil {
		return ephemeral("Couldn't create the document: " + err.Error())
	}
	return &model.CommandResponse{}
}

func (c *Handler) executeList(args *model.CommandArgs) *model.CommandResponse {
	docs, err := c.docs.ListDocs(args.UserId, args.ChannelId)
	if err != nil {
		return ephemeral("Couldn't list documents: " + err.Error())
	}
	if len(docs) == 0 {
		return ephemeral("This channel has no documents yet. Create one with `/doc new <title>`.")
	}

	var b strings.Builder
	b.WriteString("Documents in this channel (open them from the **Docs** button in the channel header):\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "* **%s** (version %d)\n", d.Title, d.Version)
	}
	return ephemeral(b.String())
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         text,
	}
}
