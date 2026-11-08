# MatterMarkdown

Shared Markdown documents for Mattermost channels. Everyone in a channel can read its documents; one person edits at a time, and every save is kept as a version.

Requires Mattermost 10.0+ on **Postgres**. Developed and tested against 12.0.0.

## Using it

- **Documents button** in the App Bar (right edge) opens the channel's document list. Create documents and folders there. The list shows one folder at a time; the breadcrumb at the top moves back up.
- **Drag to organize**: drag a row above or below another to choose its place (folders first, then documents); drop it on a folder, or on a breadcrumb entry, to file it there. The order is per folder and shared with everyone in the channel. The **⋯** menus still rename, move and delete.
- **Folders** nest. Use a folder's **⋯** menu to rename, move or delete it (only empty folders can be deleted), and a document's **⋯** menu to move or delete it. Sibling folder names must differ, ignoring case.
- **`/doc new <title>`** creates a document; **`/doc list`** lists the channel's documents.
- Creating a document posts a card in the channel; **Open** on the card opens the document.
- **Formatting** is standard Markdown (CommonMark) plus GitHub's tables, ~~strikethrough~~, task lists (`- [ ]`) and automatic links. A single line break shows as a line break, as in Mattermost messages. Raw HTML is shown as text. `@mentions`, `~channels` and `:emoji:` aren't rendered inside documents.
- **Edit** takes the edit lock. While you hold it, others see "@you is editing" and can read but not edit. The editor shows a live preview. **Ctrl/Cmd+S** saves; **Done** saves and releases the lock.
- **Sharing**: **Share…** (in the viewer, in each **⋯** menu, and on the breadcrumb for the open folder) opens a dialog with the link, `https://<site>/<team>/com.otules.mattermarkdown/doc/<id>` or `.../folder/<id>`, and a **Copy link** button. Opening a link switches to the channel and opens the document, or the list at that folder; links use IDs, so they survive renames and moves. Same-site links open inside Mattermost without reloading.
- **Channel bookmarks**: the Share dialog's **+ Add to this channel's bookmarks** adds the document (📝) or folder (📁) to the channel's bookmark bar as you, so Mattermost's bookmark permissions apply. It shows only where the bookmark bar exists (licensed servers, Entry edition included) and you may add bookmarks, and says so if the link is already bookmarked. You can still paste a link with *Add a link*.
- **History** lists every saved version. You can view any of them and restore one, which opens it in the editor for you to save.
- **Delete** is allowed for the document's creator and anyone who can delete others' posts in the channel (channel admins by default). Deleting is a soft delete: the rows stay in the database.

### Who can do what

| Action | Needs |
|---|---|
| Read, view history | `read_channel` on the channel (channel members) |
| Create, edit, manage folders, move documents | `create_post` on the channel, and the channel isn't archived |
| Delete | Creator, or `delete_others_posts` on the channel |

## How it works

- **Storage**: three tables in Mattermost's own Postgres database, `mmdocs_docs`, `mmdocs_revisions` and `mmdocs_folders`, created or upgraded on activation. Upgrading from 0.1.0 adds `folder_id` to existing documents, which stay at the top level. Upgrading from 0.3.0 adds a `sort_order` column to documents and folders; existing rows keep the order they were shown in before. Activation takes a cluster mutex so that only one node runs the migration at a time.
- **Locking**: the lock is stored in the document row and taken with a single conditional `UPDATE`, so it is safe across a cluster. Locks last 90 seconds and the editor renews them every 30. A closed tab or crashed browser frees the document within 90 seconds.
- **Saving**: a save must hold the lock *and* name the version it started from (`base_version`), so nothing is silently overwritten. Every save keeps a full copy as a version, so each person can store at most 20 MB of document content per hour, new documents included. Normal editing stays far below that; past it, saves are refused until the hour is up. The count is kept in memory and resets when the plugin restarts.
- **Live updates**: changes send a `custom_com.otules.mattermarkdown_doc_changed` WebSocket event to the channel. It carries only IDs, so clients re-fetch through the API and its permission checks.

### REST API

Base: `<site>/plugins/com.otules.mattermarkdown/api/v1`. All endpoints use the caller's Mattermost session.

| Method | Path | |
|---|---|---|
| GET / POST | `/channels/{channel_id}/docs` | List (without content) / create `{title, content}` |
| GET / PUT / DELETE | `/docs/{doc_id}` | Read / save `{base_version, title, content}` / delete |
| POST / DELETE | `/docs/{doc_id}/lock` | Take or renew the lock (409 with the document if someone else holds it) / release it |
| GET | `/docs/{doc_id}/revisions[/{version}]` | List versions / read one |
| PUT | `/docs/{doc_id}/folder` | Move `{folder_id}` (`""` for the top level); no lock needed |
| GET / POST | `/channels/{channel_id}/folders` | List all (flat; build the tree from `parent_id`) / create `{name, parent_id}` |
| GET / PATCH / DELETE | `/folders/{folder_id}` | Read / rename or move `{name?, parent_id?}` / delete (only if empty) |
| PUT | `/channels/{channel_id}/order` | Set the manual order of one folder's view: `{parent_id, folder_ids, doc_ids}`, each list in full display order; unlisted items keep their place |

Documents are created in a folder by passing `folder_id` to the create call.

Conflicts return 409 with `{"id": "locked" | "version_conflict" | "folder_name_taken" | "folder_not_empty"}`. A save or new document over the hourly budget returns 429 with `{"id": "save_budget"}`.

## Development

Needs Go (the toolchain in `go.mod` downloads automatically), Node 24 (see `.nvmrc`), and Docker for integration tests.

```bash
make dist                                   # build dist/com.otules.mattermarkdown-<version>.tar.gz
SKIP_DOCKER_TESTS=1 go test ./server/...    # server unit tests
go test ./server -run TestDocumentLifecycle # API integration test against Mattermost + Postgres in Docker
cd webapp && npx jest                       # webapp tests
make check-style                            # golangci-lint, eslint, tsc
```

Integration tests use `mattermost-enterprise-edition:latest` by default. To match a production server, set `MM_TEST_IMAGE=mattermost/mattermost-enterprise-edition:12.0.0`.

### Installing on a server

Upload `dist/com.otules.mattermarkdown-<version>.tar.gz` in **System Console → Plugins → Plugin Management** (needs `PluginSettings.EnableUploads`), or from the server:

```bash
mmctl plugin add dist/com.otules.mattermarkdown-<version>.tar.gz
mmctl plugin enable com.otules.mattermarkdown
```

`make deploy` also works with local mode or admin credentials (`MM_SERVICESETTINGS_SITEURL` plus `MM_ADMIN_TOKEN`, or `MM_ADMIN_USERNAME`/`MM_ADMIN_PASSWORD`), as in the upstream [plugin starter template](https://github.com/mattermost/mattermost-plugin-starter-template).

## Not yet built

- Simultaneous editing by several people (Yjs/CRDT). The storage layout leaves room for it.
- Diffs between versions, search across documents, @mentions inside documents, `.md` import/export (including uploading a folder of `.md` files as a MatterMarkdown folder).
- A custom view for the mobile apps. On mobile the card shows as a plain "created the document" message.

## Support

Built for and used on my own Mattermost server. Issues and pull requests are welcome, but there are no promises about fixes, features or response times.

## License

Apache License 2.0; see [LICENSE](LICENSE).

Mattermost is a trademark of Mattermost, Inc. This project is not affiliated with or endorsed by Mattermost, Inc.
