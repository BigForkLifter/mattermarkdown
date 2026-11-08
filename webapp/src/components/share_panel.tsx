import {addChannelBookmark, ApiError} from 'client';
import {copyText, docLink, folderLink} from 'links';
import React, {useEffect, useRef, useState} from 'react';
import ReactDOM from 'react-dom';
import {useSelector} from 'react-redux';

import type {GlobalState} from '@mattermost/types/store';

import {Permissions} from 'mattermost-redux/constants';
import {getLicense} from 'mattermost-redux/selectors/entities/general';
import {haveIChannelPermission} from 'mattermost-redux/selectors/entities/roles';

export type ShareTarget = {
    kind: 'doc' | 'folder';
    id: string;
    channelId: string;

    /** The title or folder name, used as the bookmark's name. */
    name: string;
};

type BookmarkState = 'idle' | 'busy' | 'added';

/** A dialog with a document or folder link, a copy button and a one-click channel bookmark. */
export default function SharePanel({target, onClose}: {target: ShareTarget; onClose: () => void}) {
    const {kind, id, channelId, name} = target;
    const link = useSelector((state: GlobalState) => (kind === 'doc' ? docLink : folderLink)(state, channelId, id));
    const canBookmark = useSelector((state: GlobalState) => bookmarkAllowed(state, channelId));
    const alreadyBookmarked = useSelector((state: GlobalState) => {
        const bookmarks = state.entities.channelBookmarks?.byChannelId[channelId];
        return Object.values(bookmarks ?? {}).some((b) => b.link_url === link);
    });

    const [copied, setCopied] = useState(false);
    const [bookmark, setBookmark] = useState<BookmarkState>('idle');
    const [error, setError] = useState('');
    const inputRef = useRef<HTMLInputElement>(null);

    // Capture Escape before anything underneath (the editor overlay closes on it too).
    useEffect(() => {
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.stopPropagation();
                onClose();
            }
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => window.removeEventListener('keydown', onKeyDown, true);
    }, [onClose]);

    useEffect(() => {
        if (!copied) {
            return undefined;
        }
        const timer = window.setTimeout(() => setCopied(false), 2000);
        return () => window.clearTimeout(timer);
    }, [copied]);

    const copy = async () => {
        setError('');
        try {
            await copyText(link);
            setCopied(true);
        } catch {
            inputRef.current?.select();
            setError('Couldn\'t copy. The link is selected above; copy it with Ctrl+C.');
        }
    };

    const addBookmark = async () => {
        setError('');
        setBookmark('busy');
        try {
            await addChannelBookmark(channelId, name, link, kind === 'doc' ? 'memo' : 'file_folder');
            setBookmark('added');
        } catch (e) {
            setBookmark('idle');
            setError(e instanceof ApiError && e.status === 403 ? 'You don\'t have permission to add bookmarks in this channel.' : `Couldn't add the bookmark: ${(e as Error).message}`);
        }
    };

    const noun = kind === 'doc' ? 'document' : 'folder';
    const opensWith = kind === 'doc' ? 'the document open' : 'the folder in the sidebar';
    const title = `Share ${noun}`;

    // A portal, so the sidebar's layout can't clip or position the dialog.
    return ReactDOM.createPortal((
        <div
            className='mmd-share-backdrop'
            onMouseDown={(e) => e.target === e.currentTarget && onClose()}
        >
            <div
                className='mmd-share'
                role='dialog'
                aria-modal='true'
                aria-label={title}
            >
                <header className='mmd-share-header'>
                    <button
                        className='mmd-close'
                        aria-label='Close'
                        onClick={onClose}
                    >{'×'}</button>
                    <h3 className='mmd-share-title'>{title}</h3>
                </header>

                <div className='mmd-share-body'>
                    <label className='mmd-share-label'>
                        {`Link to this ${noun}`}
                        <input
                            ref={inputRef}
                            className='form-control mmd-share-link'
                            readOnly={true}
                            value={link}
                            onFocus={(e) => e.target.select()}
                        />
                    </label>
                    <button
                        className='btn btn-primary mmd-share-copy'
                        autoFocus={true}
                        onClick={copy}
                    >{copied ? 'Copied' : 'Copy link'}</button>
                    <p className='mmd-share-hint'>
                        {`Anyone in this channel can open it. It opens the channel with ${opensWith}, and keeps working if the ${noun} is renamed or moved.`}
                    </p>

                    {canBookmark && (
                        <>
                            <h4 className='mmd-share-section'>{'Channel bookmark'}</h4>
                            {alreadyBookmarked || bookmark === 'added' ? (
                                <div
                                    className='mmd-share-bookmarked'
                                    role='status'
                                >{bookmark === 'added' ? 'Added to this channel\'s bookmarks.' : 'Already in this channel\'s bookmarks.'}</div>
                            ) : (
                                <>
                                    <button
                                        className='btn btn-tertiary mmd-share-bookmark'
                                        disabled={bookmark === 'busy'}
                                        onClick={addBookmark}
                                    >{'+ Add to this channel\'s bookmarks'}</button>
                                    <p className='mmd-share-hint'>{'Or paste the link in the bookmarks bar with "Add a link".'}</p>
                                </>
                            )}
                        </>
                    )}

                    {error && <div className='mmd-error'>{error}</div>}
                </div>
            </div>
        </div>
    ), document.body);
}

/**
 * Channel bookmarks only exist on licensed servers (the web app hides the bar otherwise). In
 * public and private channels adding one is a channel permission; in DMs and group messages
 * every member may. The server checks again.
 */
function bookmarkAllowed(state: GlobalState, channelId: string): boolean {
    if (getLicense(state).IsLicensed !== 'true') {
        return false;
    }
    const channel = state.entities.channels.channels[channelId];
    if (!channel || channel.delete_at) {
        return false;
    }
    if (channel.type === 'D' || channel.type === 'G') {
        return true;
    }
    const permission = channel.type === 'O' ? Permissions.ADD_BOOKMARK_PUBLIC_CHANNEL : Permissions.ADD_BOOKMARK_PRIVATE_CHANNEL;
    return haveIChannelPermission(state, channel.team_id, channelId, permission);
}
