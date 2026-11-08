import manifest from 'manifest';
import type {UnknownAction} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

export type DocChangedEvent = {
    doc_id: string;
    channel_id: string;
    action: 'created' | 'updated' | 'locked' | 'unlocked' | 'deleted' | 'moved' | 'reordered' |
    'folder_created' | 'folder_updated' | 'folder_deleted' | 'folder_reordered';

    /** Increments on every event so components can react to repeats. */
    seq: number;
};

/** The folder the document list shows. Tied to a channel so switching channels starts at the top. */
export type ListFolder = {channelId: string; folderId: string};

type PluginState = {
    openDoc: {id: string; startEditing: boolean} | null;
    lastEvent: DocChangedEvent | null;
    listFolder: ListFolder | null;
};

// Literal types, so the reducer can narrow on them.
const OPEN_DOC = 'mattermarkdown_open_doc';
const CLOSE_DOC = 'mattermarkdown_close_doc';
const DOC_CHANGED = 'mattermarkdown_doc_changed';
const SHOW_FOLDER = 'mattermarkdown_show_folder';

type Action =
    | {type: typeof OPEN_DOC; id: string; startEditing: boolean}
    | {type: typeof CLOSE_DOC}
    | {type: typeof DOC_CHANGED; event: Omit<DocChangedEvent, 'seq'>}
    | {type: typeof SHOW_FOLDER; listFolder: ListFolder};

export const openDoc = (id: string, startEditing = false) => ({type: OPEN_DOC, id, startEditing});
export const closeDoc = () => ({type: CLOSE_DOC});
export const docChanged = (event: Omit<DocChangedEvent, 'seq'>) => ({type: DOC_CHANGED, event});
export const showFolder = (channelId: string, folderId: string) => ({type: SHOW_FOLDER, listFolder: {channelId, folderId}});

const initialState: PluginState = {openDoc: null, lastEvent: null, listFolder: null};

export function reducer(state: PluginState = initialState, unknownAction: UnknownAction): PluginState {
    const action = unknownAction as Action;
    switch (action.type) {
    case OPEN_DOC:
        return {...state, openDoc: {id: action.id, startEditing: action.startEditing}};
    case CLOSE_DOC:
        return {...state, openDoc: null};
    case DOC_CHANGED:
        return {...state, lastEvent: {...action.event, seq: (state.lastEvent?.seq ?? 0) + 1}};
    case SHOW_FOLDER:
        return {...state, listFolder: action.listFolder};
    default:
        return state;
    }
}

const pluginState = (state: GlobalState): PluginState => (state as any)[`plugins-${manifest.id}`] ?? initialState;

export const selectOpenDoc = (state: GlobalState) => pluginState(state).openDoc;
export const selectLastEvent = (state: GlobalState) => pluginState(state).lastEvent;

/** The folder to show for a channel: '' (top level) unless the list was pointed at one of its folders. */
export const selectListFolderId = (state: GlobalState, channelId: string) => {
    const listFolder = pluginState(state).listFolder;
    return listFolder?.channelId === channelId ? listFolder.folderId : '';
};
