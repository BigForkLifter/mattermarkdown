import manifest from 'manifest';

import {Client4} from 'mattermost-redux/client';

export type Doc = {
    id: string;
    channel_id: string;

    /** Empty for the channel's top level. May name a folder that no longer exists; treat that as top level. */
    folder_id: string;

    /** Manual position among the folder's documents; lower comes first. */
    sort_order: number;
    title: string;
    content?: string;
    version: number;
    created_by: string;
    created_at: number;
    updated_by: string;
    updated_at: number;

    /** Empty unless someone holds an unexpired edit lock. */
    lock_user_id: string;
    lock_expires_at: number;
};

export type Revision = {
    doc_id: string;
    version: number;
    title: string;
    content?: string;
    created_by: string;
    created_at: number;
};

export type Folder = {
    id: string;
    channel_id: string;

    /** Empty for the channel's top level. */
    parent_id: string;

    /** Manual position among its sibling folders; lower comes first. */
    sort_order: number;
    name: string;
    created_by: string;
    created_at: number;
};

export class ApiError extends Error {
    status: number;

    /** For 409s: "locked", "version_conflict", "folder_name_taken" or "folder_not_empty". */
    id: string;

    /** The document returned with a failed lock attempt, naming who holds the lock. */
    doc?: Doc;

    constructor(message: string, status: number, id = '', doc?: Doc) {
        super(message);
        this.status = status;
        this.id = id;
        this.doc = doc;
    }
}

let siteUrl = '';
let baseUrl = `/plugins/${manifest.id}/api/v1`;

export function setSiteUrl(url: string) {
    siteUrl = url.replace(/\/+$/, '');
    baseUrl = `${siteUrl}/plugins/${manifest.id}/api/v1`;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const response = await fetch(baseUrl + path, Client4.getOptions({
        method,
        body: body === undefined ? undefined : JSON.stringify(body),
    }));
    if (response.status === 204) {
        return undefined as T;
    }

    const data = await response.json().catch(() => ({}));
    if (!response.ok) {
        // A refused lock returns the document itself rather than an error body.
        if (response.status === 409 && data.id && data.lock_user_id !== undefined) {
            throw new ApiError('Someone else is editing this document', 409, 'locked', data);
        }
        throw new ApiError(data.error || `Request failed (${response.status})`, response.status, data.id || '');
    }
    return data as T;
}

export const listDocs = (channelId: string) => request<Doc[]>('GET', `/channels/${channelId}/docs`);
export const createDoc = (channelId: string, title: string, folderId = '') =>
    request<Doc>('POST', `/channels/${channelId}/docs`, {title, content: '', folder_id: folderId});
export const moveDoc = (docId: string, folderId: string) => request<Doc>('PUT', `/docs/${docId}/folder`, {folder_id: folderId});
export const getDoc = (docId: string) => request<Doc>('GET', `/docs/${docId}`);
export const saveDoc = (docId: string, baseVersion: number, title: string, content: string) =>
    request<Doc>('PUT', `/docs/${docId}`, {base_version: baseVersion, title, content});
export const deleteDoc = (docId: string) => request<void>('DELETE', `/docs/${docId}`);
export const lockDoc = (docId: string) => request<Doc>('POST', `/docs/${docId}/lock`);
export const unlockDoc = (docId: string) => request<void>('DELETE', `/docs/${docId}/lock`);
export const listRevisions = (docId: string) => request<Revision[]>('GET', `/docs/${docId}/revisions`);
export const getRevision = (docId: string, version: number) => request<Revision>('GET', `/docs/${docId}/revisions/${version}`);

export const listFolders = (channelId: string) => request<Folder[]>('GET', `/channels/${channelId}/folders`);
export const createFolder = (channelId: string, name: string, parentId = '') =>
    request<Folder>('POST', `/channels/${channelId}/folders`, {name, parent_id: parentId});
export const getFolder = (folderId: string) => request<Folder>('GET', `/folders/${folderId}`);
export const updateFolder = (folderId: string, patch: {name?: string; parent_id?: string}) =>
    request<Folder>('PATCH', `/folders/${folderId}`, patch);
export const deleteFolder = (folderId: string) => request<void>('DELETE', `/folders/${folderId}`);
export const setOrder = (channelId: string, parentId: string, folderIds: string[], docIds: string[]) =>
    request<void>('PUT', `/channels/${channelId}/order`, {parent_id: parentId, folder_ids: folderIds, doc_ids: docIds});

/**
 * Adds a link bookmark to a channel as the current user, through Mattermost's own API, so the
 * server applies its bookmark permissions and limits. Channel bookmarks need a licensed server.
 */
export async function addChannelBookmark(channelId: string, displayName: string, linkUrl: string, emoji: string): Promise<void> {
    const response = await fetch(`${siteUrl}/api/v4/channels/${channelId}/bookmarks`, Client4.getOptions({
        method: 'POST',
        body: JSON.stringify({type: 'link', display_name: displayName, link_url: linkUrl, emoji}),
    }));
    if (!response.ok) {
        const data = await response.json().catch(() => ({}));
        throw new ApiError(data.message || `Request failed (${response.status})`, response.status, data.id || '');
    }
}
