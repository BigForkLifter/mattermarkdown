import manifest from 'manifest';

import type {Channel} from '@mattermost/types/channels';
import type {GlobalState} from '@mattermost/types/store';

import {Client4} from 'mattermost-redux/client';
import {getConfig} from 'mattermost-redux/selectors/entities/general';
import {getCurrentTeam} from 'mattermost-redux/selectors/entities/teams';

/**
 * Links have the form <site>/<team>/com.otules.mattermarkdown/doc/<id> (or /folder/<id>). Mattermost serves
 * them through the routes registered in index.tsx, and opens same-site links from posts and
 * channel bookmarks inside the app.
 */
export function docLink(state: GlobalState, channelId: string, docId: string): string {
    return `${siteUrl(state)}/${teamNameFor(state, channelId)}/${manifest.id}/doc/${docId}`;
}

export function folderLink(state: GlobalState, channelId: string, folderId: string): string {
    return `${siteUrl(state)}/${teamNameFor(state, channelId)}/${manifest.id}/folder/${folderId}`;
}

function siteUrl(state: GlobalState): string {
    return (getConfig(state).SiteURL || window.location.origin).replace(/\/+$/, '');
}

/** The channel's team, or the current team for DMs and group messages, which have none. */
function teamNameFor(state: GlobalState, channelId: string, channel?: Channel): string {
    const teamId = (channel ?? state.entities.channels.channels[channelId])?.team_id;
    return (teamId && state.entities.teams.teams[teamId]?.name) || getCurrentTeam(state)?.name || '';
}

/** The app path of a channel, fetching it if this client hasn't loaded it. */
export async function channelPath(state: GlobalState, channelId: string, fallbackTeam: string): Promise<string> {
    const channel = state.entities.channels.channels[channelId] ?? await Client4.getChannel(channelId);

    // Mattermost resolves DM and group names ("id__id") under /channels/ too.
    return `/${teamNameFor(state, channelId, channel) || fallbackTeam}/channels/${channel.name}`;
}

/** Copies text, falling back to execCommand where the Clipboard API is unavailable (plain-HTTP sites). */
export async function copyText(text: string): Promise<void> {
    if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        return;
    }
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.select();
    try {
        if (!document.execCommand('copy')) {
            throw new Error('Copy failed');
        }
    } finally {
        textarea.remove();
    }
}
