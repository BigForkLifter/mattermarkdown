import {getDoc, getFolder} from 'client';
import {channelPath} from 'links';
import {openDoc, showFolder} from 'plugin_state';
import React, {useEffect, useState} from 'react';
import {useDispatch, useStore} from 'react-redux';
import {useHistory, useLocation} from 'react-router-dom';
import type {UnknownAction} from 'redux';

type Target = {channelId: string; open: () => void};

/**
 * Shown at /<team>/com.otules.mattermarkdown/doc/<id> and /folder/<id>: looks the target up, switches to its
 * channel, then opens it. The URL is replaced, so Back returns to where the link was clicked.
 */
function LinkRoute({resolve}: {resolve: (id: string) => Promise<Target>}) {
    const store = useStore();
    const history = useHistory();
    const {pathname} = useLocation();
    const [error, setError] = useState('');

    useEffect(() => {
        const [team, , , id] = pathname.split('/').filter(Boolean);
        if (!id) {
            setError('This link is incomplete.');
            return;
        }
        resolve(id).then(async (target) => {
            history.replace(await channelPath(store.getState() as any, target.channelId, team));
            target.open();
        }).catch(() => setError('This link points to something that was deleted, or that you don\'t have access to.'));

        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pathname]);

    return (
        <div className='mmd-link-route'>
            {error ? (
                <>
                    <p>{error}</p>
                    <button
                        className='btn btn-tertiary'
                        onClick={() => history.goBack()}
                    >{'Go back'}</button>
                </>
            ) : (
                <p>{'Opening…'}</p>
            )}
        </div>
    );
}

export function DocLinkRoute() {
    const dispatch = useDispatch();
    return (
        <LinkRoute
            resolve={async (id) => {
                const doc = await getDoc(id);
                return {channelId: doc.channel_id, open: () => dispatch(openDoc(doc.id))};
            }}
        />
    );
}

/** showList opens the document list (the RHS); it comes from the RHS registration. */
export function makeFolderLinkRoute(showList: UnknownAction) {
    return function FolderLinkRoute() {
        const dispatch = useDispatch();
        return (
            <LinkRoute
                resolve={async (id) => {
                    const folder = await getFolder(id);
                    return {
                        channelId: folder.channel_id,
                        open: () => {
                            dispatch(showFolder(folder.channel_id, folder.id));
                            dispatch(showList);
                        },
                    };
                }}
            />
        );
    };
}
