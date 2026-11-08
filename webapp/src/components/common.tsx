import {renderMarkdown} from 'markdown';
import React, {useEffect, useMemo} from 'react';
import {useDispatch, useSelector} from 'react-redux';

import type {GlobalState} from '@mattermost/types/store';

import {getMissingProfilesByIds} from 'mattermost-redux/actions/users';

/** Shows @username, fetching the profile if this client hasn't loaded it yet. */
export function Username({userId}: {userId: string}) {
    const dispatch = useDispatch();
    const user = useSelector((state: GlobalState) => state.entities.users.profiles[userId]);
    useEffect(() => {
        if (!user && userId) {
            dispatch(getMissingProfilesByIds([userId]) as any);
        }
    }, [dispatch, user, userId]);
    return <span className='mmd-username'>{user ? `@${user.username}` : 'someone'}</span>;
}

type MarkdownViewProps = {
    text: string;

    /** Called with the app path when a link to this Mattermost is clicked, instead of reloading the page. */
    onInternalLink?: (path: string) => void;
};

/** Renders a document as standard Markdown; see markdown.ts. */
export function MarkdownView({text, onInternalLink}: MarkdownViewProps) {
    const html = useMemo(() => ({__html: renderMarkdown(text)}), [text]);

    const onClick = (e: React.MouseEvent) => {
        const anchor = (e.target as HTMLElement).closest('a');
        if (!anchor || !onInternalLink || anchor.target || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) {
            return;
        }
        const url = new URL(anchor.href, window.location.href);
        if (url.origin === window.location.origin) {
            e.preventDefault();
            onInternalLink(url.pathname + url.search + url.hash);
        }
    };

    if (!text.trim()) {
        return <div className='mmd-markdown mmd-empty'>{'This document is empty.'}</div>;
    }
    return (
        <div
            className='mmd-markdown'
            onClick={onClick}

            // Safe: markdown-it escapes raw HTML and refuses javascript: links (html: false).
            dangerouslySetInnerHTML={html}
        />
    );
}

export function DocIcon({size = 16}: {size?: number}) {
    return (
        <svg
            width={size}
            height={size}
            viewBox='0 0 24 24'
            fill='none'
            stroke='currentColor'
            strokeWidth='1.8'
            strokeLinecap='round'
            strokeLinejoin='round'
            aria-hidden='true'
        >
            <path d='M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z'/>
            <path d='M14 2v6h6'/>
            <path d='M7.5 17v-5l2 2.5 2-2.5v5'/>
            <path d='M15.5 12v5m-1.6-1.6 1.6 1.6 1.6-1.6'/>
        </svg>
    );
}

export function FolderIcon({size = 16}: {size?: number}) {
    return (
        <svg
            width={size}
            height={size}
            viewBox='0 0 24 24'
            fill='none'
            stroke='currentColor'
            strokeWidth='1.8'
            strokeLinecap='round'
            strokeLinejoin='round'
            aria-hidden='true'
        >
            <path d='M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z'/>
        </svg>
    );
}
