import {openDoc} from 'plugin_state';
import React from 'react';
import {useDispatch} from 'react-redux';

import type {Post} from '@mattermost/types/posts';

import {DocIcon} from './common';

/** Renders the post made when a document is created, with a button to open it. */
export default function DocCard({post}: {post: Post}) {
    const dispatch = useDispatch();
    const docId = String(post.props?.doc_id ?? '');
    const title = String(post.props?.doc_title ?? 'Untitled');

    return (
        <div className='mmd-card-post'>
            <div className='mmd-card-text'>{'created a document'}</div>
            <button
                className='mmd-card'
                onClick={() => docId && dispatch(openDoc(docId))}
            >
                <span className='mmd-card-icon'><DocIcon size={22}/></span>
                <span className='mmd-card-title'>{title}</span>
                <span className='mmd-card-open'>{'Open'}</span>
            </button>
        </div>
    );
}
