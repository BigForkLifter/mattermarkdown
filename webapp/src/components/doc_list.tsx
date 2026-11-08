import {ApiError, createDoc, createFolder, deleteDoc, deleteFolder, listDocs, listFolders, moveDoc, setOrder, updateFolder} from 'client';
import type {Doc, Folder} from 'client';
import {folderPath, indexFolders, moveTargets, reordered} from 'folders';
import {openDoc, selectLastEvent, selectListFolderId, showFolder} from 'plugin_state';
import React, {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {useDispatch, useSelector} from 'react-redux';
import {relativeTime} from 'time';

import type {GlobalState} from '@mattermost/types/store';

import {getCurrentChannelId} from 'mattermost-redux/selectors/entities/common';

import {DocIcon, FolderIcon, Username} from './common';
import SharePanel from './share_panel';
import type {ShareTarget} from './share_panel';

type Form =
    | {kind: 'new-doc'}
    | {kind: 'new-folder'}
    | {kind: 'rename'; folder: Folder}
    | {kind: 'move-doc'; doc: Doc}
    | {kind: 'move-folder'; folder: Folder};

type DragKind = 'doc' | 'folder';
type DragItem = {kind: DragKind; id: string};

// A drop position: between siblings, or into a folder.
type DropHint =
    | {kind: 'insert'; target: DragItem; before: boolean}
    | {kind: 'into'; folderId: string}
    | null;

/** The right-hand sidebar: the current channel's folders and documents, one folder at a time. */
export default function DocList() {
    const dispatch = useDispatch();
    const channelId = useSelector(getCurrentChannelId);
    const lastEvent = useSelector(selectLastEvent);
    const requestedFolderId = useSelector((state: GlobalState) => selectListFolderId(state, channelId));

    const [data, setData] = useState<{docs: Doc[]; folders: Folder[]} | null>(null);
    const [error, setError] = useState('');
    const [form, setForm] = useState<Form | null>(null);
    const [sharing, setSharing] = useState<ShareTarget | null>(null);
    const stopSharing = useCallback(() => setSharing(null), []);

    // Drag state. The refs mirror the "hint" and "dragging" state so the high-frequency
    // dragover and the drop handler read current values without re-rendering.
    const dragRef = useRef<DragItem | null>(null);
    const hintRef = useRef<DropHint>(null);
    const [dragging, setDragging] = useState<DragItem | null>(null);
    const [hint, setHint] = useState<DropHint>(null);
    const setDropHint = useCallback((h: DropHint) => {
        hintRef.current = h;
        setHint(h);
    }, []);

    const eventForChannel = lastEvent && (lastEvent.channel_id === channelId || !lastEvent.channel_id) ? lastEvent.seq : 0;

    useEffect(() => {
        if (!channelId) {
            return undefined;
        }

        // Ignore a slow response for a channel the user has already left, and don't refresh
        // under a live drag: the optimistic order would jump mid-gesture.
        if (dragRef.current) {
            return undefined;
        }
        let current = true;
        Promise.all([listDocs(channelId), listFolders(channelId)]).then(([docs, folders]) => {
            if (current) {
                setData({docs, folders});
                setError('');
            }
        }).catch((e) => current && setError(e.message));
        return () => {
            current = false;
        };
    }, [channelId, eventForChannel]);

    useEffect(() => {
        setData(null);
        setForm(null);
        setSharing(null);
        dragRef.current = null;
        hintRef.current = null;
        setDragging(null);
        setHint(null);
    }, [channelId]);

    const index = useMemo(() => indexFolders(data?.folders ?? []), [data]);

    // A folder deleted by someone else, or not loaded yet, falls back to the top level.
    const folderId = index.byId.has(requestedFolderId) ? requestedFolderId : '';
    const path = folderPath(index, folderId);
    const subfolders = index.children.get(folderId) ?? [];
    const placeOf = (doc: Doc) => (index.byId.has(doc.folder_id) ? doc.folder_id : '');
    const docsHere = (data?.docs ?? []).filter((d) => placeOf(d) === folderId);
    const itemCount = (folder: Folder) => (index.children.get(folder.id)?.length ?? 0) + (data?.docs ?? []).filter((d) => placeOf(d) === folder.id).length;

    const goTo = (id: string) => {
        setForm(null);
        dispatch(showFolder(channelId, id));
    };

    const run = async (action: () => Promise<unknown>) => {
        setError('');
        try {
            await action();
            setForm(null);
        } catch (e) {
            setError(e instanceof ApiError && e.id === 'folder_not_empty' ? 'Only empty folders can be deleted. Move or delete what\'s inside first.' : (e as Error).message);
        }
    };

    // Re-fetches the lists, e.g. to undo an optimistic reorder whose save failed.
    const reload = useCallback(() => {
        if (!channelId) {
            return;
        }
        Promise.all([listDocs(channelId), listFolders(channelId)]).then(([docs, folders]) => {
            if (!dragRef.current) {
                setData({docs, folders});
            }
        }).catch(() => {
            // Nothing useful to add to the error already shown; the next event retries.
        });
    }, [channelId]);

    const share = (kind: ShareTarget['kind'], id: string, name: string) => setSharing({kind, id, channelId, name});

    const removeFolder = (folder: Folder) => {
        if (window.confirm(`Delete the folder "${folder.name}"?`)) { // eslint-disable-line no-alert
            run(() => deleteFolder(folder.id));
        }
    };

    // Same confirmation and permission message as the editor's Delete. The server allows the
    // creator and channel admins, and its "deleted" event refreshes every open list.
    const removeDoc = async (doc: Doc) => {
        const editing = doc.lock_user_id ? ' Someone is editing it right now; their unsaved changes will be lost.' : '';
        if (!window.confirm(`Delete "${doc.title}"? Its history is kept on the server, but it disappears for everyone.${editing}`)) { // eslint-disable-line no-alert
            return;
        }
        setError('');
        try {
            await deleteDoc(doc.id);
            reload();
        } catch (e) {
            setError(e instanceof ApiError && e.status === 403 ? 'Only the document\'s creator or a channel admin can delete it.' : (e as Error).message);
        }
    };

    const parentOf = (folder: Folder) => (index.byId.has(folder.parent_id) ? folder.parent_id : '');

    /** Files a dragged row into a folder (or the top level), through the same calls as the Move to… picker. */
    const fileInto = (drag: DragItem, targetFolderId: string) => {
        run(() => (drag.kind === 'doc' ? moveDoc(drag.id, targetFolderId) : updateFolder(drag.id, {parent_id: targetFolderId})));
    };

    const reorder = async (kind: DragKind, next: Array<Doc | Folder>) => {
        if (!data) {
            return;
        }
        const nextIds = next.map((x) => x.id);
        const currentIds = kind === 'folder' ?
            data.folders.filter((f) => parentOf(f) === folderId).map((f) => f.id) :
            data.docs.filter((d) => placeOf(d) === folderId).map((d) => d.id);
        if (nextIds.join() === currentIds.join()) {
            return;
        }

        // Optimistic: the reordered siblings go back into their old slots of the full list.
        setData((prev) => {
            if (!prev) {
                return prev;
            }
            if (kind === 'folder') {
                let i = 0;
                const folders = prev.folders.map((f) => (parentOf(f) === folderId ? (next as Folder[])[i++] : f));
                return {...prev, folders};
            }
            let i = 0;
            const docs = prev.docs.map((d) => (placeOf(d) === folderId ? (next as Doc[])[i++] : d));
            return {...prev, docs};
        });
        try {
            await setOrder(channelId, folderId, kind === 'folder' ? nextIds : [], kind === 'doc' ? nextIds : []);
        } catch (e) {
            setError((e as Error).message);
            reload();
        }
    };

    const startDrag = (item: DragItem) => (e: React.DragEvent) => {
        dragRef.current = item;
        setDragging(item);
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', item.id); // Firefox won't start a drag without data
    };

    const endDrag = () => {
        dragRef.current = null;
        setDragging(null);
        setDropHint(null);
    };

    /** dragover on a list row. A document hovers a folder row to file into it; the edges of a
     * folder row, or anywhere on a document row, position the row between its siblings. */
    const rowDragOver = (target: DragItem) => (e: React.DragEvent) => {
        const drag = dragRef.current;
        const sameKind = drag?.kind === target.kind;
        const docIntoFolder = drag?.kind === 'doc' && target.kind === 'folder';
        if (!drag || (!sameKind && !docIntoFolder)) {
            return; // a folder can't be dropped among documents
        }
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        const rect = e.currentTarget.getBoundingClientRect();
        const y = (e.clientY - rect.top) / Math.max(rect.height, 1);
        if (target.kind === 'folder' && target.id !== drag.id && (drag.kind === 'doc' || (y > 0.3 && y < 0.7))) {
            setDropHint({kind: 'into', folderId: target.id});
        } else if (target.id === drag.id) {
            setDropHint(null);
        } else {
            setDropHint({kind: 'insert', target, before: y < 0.5});
        }
    };

    // The hint mirrors the last dragover, which is the row the drop lands on.
    const rowDrop = () => (e: React.DragEvent) => {
        e.preventDefault();
        const drag = dragRef.current;
        const h = hintRef.current;
        endDrag();
        if (!drag || !h) {
            return;
        }
        if (h.kind === 'into') {
            fileInto(drag, h.folderId);
            return;
        }
        if (h.target.kind !== drag.kind) {
            return;
        }
        const group = h.target.kind === 'folder' ? subfolders : docsHere;
        const from = group.findIndex((x) => x.id === drag.id);
        const anchor = group.findIndex((x) => x.id === h.target.id);
        if (from < 0 || anchor < 0) {
            return;
        }
        const slot = h.before ? anchor : anchor + 1;
        const next = h.target.kind === 'folder' ? reordered(subfolders, from, slot) : reordered(docsHere, from, slot);
        reorder(h.target.kind, next);
    };

    // Dropping on a breadcrumb folder files the row into it (or into the top level).
    const crumbDragOver = (crumbId: string) => (e: React.DragEvent) => {
        if (!dragRef.current) {
            return;
        }
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        setDropHint({kind: 'into', folderId: crumbId});
    };

    const crumbDrop = (crumbId: string) => (e: React.DragEvent) => {
        e.preventDefault();
        const drag = dragRef.current;
        endDrag();
        if (drag) {
            fileInto(drag, crumbId);
        }
    };

    const rowClass = (item: DragItem) => {
        let cls = '';
        if (dragging && dragging.kind === item.kind && dragging.id === item.id) {
            cls += ' mmd-dragging';
        }
        if (hint?.kind === 'into') {
            cls += hint.folderId === item.id && item.kind === 'folder' ? ' mmd-drop-into' : '';
        } else if (hint && hint.target.kind === item.kind && hint.target.id === item.id) {
            cls += hint.before ? ' mmd-drop-before' : ' mmd-drop-after';
        }
        return cls;
    };

    return (
        <div className='mmd-rhs'>
            <nav
                className='mmd-breadcrumb'
                aria-label='Folder'
            >
                {[{id: '', name: 'All documents'}, ...path].map((f, i, all) => (
                    <React.Fragment key={f.id || 'top'}>
                        {i > 0 && <span className='mmd-breadcrumb-sep'>{'›'}</span>}
                        {i === all.length - 1 ? (
                            <span className='mmd-breadcrumb-current'>{f.name}</span>
                        ) : (
                            <button
                                className={'mmd-breadcrumb-link' + (hint?.kind === 'into' && hint.folderId === f.id ? ' mmd-drop-into' : '')}
                                onClick={() => goTo(f.id)}
                                onDragOver={crumbDragOver(f.id)}
                                onDrop={crumbDrop(f.id)}
                            >{f.name}</button>
                        )}
                    </React.Fragment>
                ))}
                {folderId && (
                    <button
                        className='mmd-breadcrumb-copy'
                        title='Copy a link to this folder or add it to the channel bookmarks'
                        onClick={() => share('folder', folderId, path[path.length - 1].name)}
                    >{'Share…'}</button>
                )}
            </nav>

            {form?.kind === 'new-doc' && (
                <NameForm
                    placeholder='Document title'
                    submitLabel='Create'
                    maxLength={256}
                    onCancel={() => setForm(null)}
                    onSubmit={(title) => run(async () => {
                        const doc = await createDoc(channelId, title, folderId);
                        dispatch(openDoc(doc.id, true));
                    })}
                />
            )}
            {form?.kind === 'new-folder' && (
                <NameForm
                    placeholder='Folder name'
                    submitLabel='Create'
                    maxLength={128}
                    onCancel={() => setForm(null)}
                    onSubmit={(name) => run(() => createFolder(channelId, name, folderId))}
                />
            )}
            {form?.kind === 'rename' && (
                <NameForm
                    placeholder='Folder name'
                    submitLabel='Rename'
                    maxLength={128}
                    initialValue={form.folder.name}
                    onCancel={() => setForm(null)}
                    onSubmit={(name) => run(() => updateFolder(form.folder.id, {name}))}
                />
            )}
            {form?.kind === 'move-doc' && (
                <MoveForm
                    label={`Move "${form.doc.title}" to`}
                    targets={moveTargets(index)}
                    current={placeOf(form.doc)}
                    onCancel={() => setForm(null)}
                    onSubmit={(target) => run(() => moveDoc(form.doc.id, target))}
                />
            )}
            {form?.kind === 'move-folder' && (
                <MoveForm
                    label={`Move "${form.folder.name}" to`}
                    targets={moveTargets(index, form.folder.id)}
                    current={index.byId.has(form.folder.parent_id) ? form.folder.parent_id : ''}
                    onCancel={() => setForm(null)}
                    onSubmit={(target) => run(() => updateFolder(form.folder.id, {parent_id: target}))}
                />
            )}
            {!form && (
                <div className='mmd-new-buttons'>
                    <button
                        className='btn btn-primary btn-sm'
                        onClick={() => setForm({kind: 'new-doc'})}
                    >{'New document'}</button>
                    <button
                        className='btn btn-tertiary btn-sm'
                        onClick={() => setForm({kind: 'new-folder'})}
                    >{'New folder'}</button>
                </div>
            )}

            {error && <div className='mmd-error mmd-rhs-error'>{error}</div>}

            {data === null && !error && <div className='mmd-rhs-empty'>{'Loading…'}</div>}
            {data && !subfolders.length && !docsHere.length && (
                <div className='mmd-rhs-empty'>
                    {folderId ? 'This folder is empty.' : (
                        <>
                            {'No documents in this channel yet. Create one here, or type '}
                            <code>{'/doc new <title>'}</code>
                            {'.'}
                        </>
                    )}
                </div>
            )}

            <ul className='mmd-doc-list'>
                {subfolders.map((folder) => (
                    <li
                        key={folder.id}
                        className={'mmd-row' + rowClass({kind: 'folder', id: folder.id})}
                        onDragOver={rowDragOver({kind: 'folder', id: folder.id})}
                        onDrop={rowDrop()}
                    >
                        <button
                            className='mmd-doc-item'
                            draggable={true}
                            onDragStart={startDrag({kind: 'folder', id: folder.id})}
                            onDragEnd={endDrag}
                            onClick={() => goTo(folder.id)}
                        >
                            <span className='mmd-doc-icon mmd-folder-icon'><FolderIcon/></span>
                            <span className='mmd-doc-text'>
                                <span className='mmd-doc-title'>{folder.name}</span>
                                <span className='mmd-doc-meta'>{plural(itemCount(folder), 'item')}</span>
                            </span>
                        </button>
                        <RowMenu
                            items={[
                                ['Share…', () => share('folder', folder.id, folder.name)],
                                ['Rename', () => setForm({kind: 'rename', folder})],
                                ['Move to…', () => setForm({kind: 'move-folder', folder})],
                                ['Delete', () => removeFolder(folder), true],
                            ]}
                        />
                    </li>
                ))}
                {docsHere.map((doc) => (
                    <li
                        key={doc.id}
                        className={'mmd-row' + rowClass({kind: 'doc', id: doc.id})}
                        onDragOver={rowDragOver({kind: 'doc', id: doc.id})}
                        onDrop={rowDrop()}
                    >
                        <button
                            className='mmd-doc-item'
                            draggable={true}
                            onDragStart={startDrag({kind: 'doc', id: doc.id})}
                            onDragEnd={endDrag}
                            onClick={() => dispatch(openDoc(doc.id))}
                        >
                            <span className='mmd-doc-icon'><DocIcon/></span>
                            <span className='mmd-doc-text'>
                                <span className='mmd-doc-title'>{doc.title}</span>
                                <span className='mmd-doc-meta'>
                                    {doc.lock_user_id ? (
                                        <span className='mmd-editing'>
                                            <Username userId={doc.lock_user_id}/>
                                            {' is editing'}
                                        </span>
                                    ) : (
                                        <>
                                            {`Updated ${relativeTime(doc.updated_at)} by `}
                                            <Username userId={doc.updated_by}/>
                                        </>
                                    )}
                                </span>
                            </span>
                        </button>
                        <RowMenu
                            items={[
                                ['Share…', () => share('doc', doc.id, doc.title)],
                                ['Move to…', () => setForm({kind: 'move-doc', doc})],
                                ['Delete', () => removeDoc(doc), true],
                            ]}
                        />
                    </li>
                ))}
            </ul>

            {sharing && (
                <SharePanel
                    target={sharing}
                    onClose={stopSharing}
                />
            )}
        </div>
    );
}

function plural(n: number, word: string) {
    return `${n} ${word}${n === 1 ? '' : 's'}`;
}

type NameFormProps = {
    placeholder: string;
    submitLabel: string;
    maxLength: number;
    initialValue?: string;
    onCancel: () => void;
    onSubmit: (value: string) => Promise<void>;
};

function NameForm({placeholder, submitLabel, maxLength, initialValue = '', onCancel, onSubmit}: NameFormProps) {
    const [value, setValue] = useState(initialValue);
    const [busy, setBusy] = useState(false);

    const submit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!value.trim() || busy) {
            return;
        }
        setBusy(true);
        await onSubmit(value);
        setBusy(false);
    };

    return (
        <form
            className='mmd-new-form'
            onSubmit={submit}
        >
            <input
                className='form-control'
                autoFocus={true}
                placeholder={placeholder}
                maxLength={maxLength}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={(e) => e.key === 'Escape' && onCancel()}
            />
            <div className='mmd-new-form-actions'>
                <button
                    type='button'
                    className='btn btn-tertiary btn-sm'
                    onClick={onCancel}
                >{'Cancel'}</button>
                <button
                    type='submit'
                    className='btn btn-primary btn-sm'
                    disabled={busy || !value.trim()}
                >{submitLabel}</button>
            </div>
        </form>
    );
}

type MoveFormProps = {
    label: string;
    targets: Array<[string, string]>;
    current: string;
    onCancel: () => void;
    onSubmit: (folderId: string) => Promise<void>;
};

function MoveForm({label, targets, current, onCancel, onSubmit}: MoveFormProps) {
    const [target, setTarget] = useState(current);
    const [busy, setBusy] = useState(false);

    const submit = async (e: React.FormEvent) => {
        e.preventDefault();
        setBusy(true);
        await onSubmit(target);
        setBusy(false);
    };

    return (
        <form
            className='mmd-new-form'
            onSubmit={submit}
        >
            <label className='mmd-move-label'>
                {label}
                <select
                    className='form-control'
                    autoFocus={true}
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                    onKeyDown={(e) => e.key === 'Escape' && onCancel()}
                >
                    {targets.map(([id, name]) => (
                        <option
                            key={id || 'top'}
                            value={id}
                        >{name}</option>
                    ))}
                </select>
            </label>
            <div className='mmd-new-form-actions'>
                <button
                    type='button'
                    className='btn btn-tertiary btn-sm'
                    onClick={onCancel}
                >{'Cancel'}</button>
                <button
                    type='submit'
                    className='btn btn-primary btn-sm'
                    disabled={busy || target === current}
                >{'Move'}</button>
            </div>
        </form>
    );
}

/** A "⋯" button with a small menu of actions; danger items are styled as destructive. */
function RowMenu({items}: {items: Array<[string, () => void, boolean?]>}) {
    const [open, setOpen] = useState(false);
    const ref = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!open) {
            return undefined;
        }
        const close = (e: Event) => {
            if (e instanceof KeyboardEvent ? e.key === 'Escape' : !ref.current?.contains(e.target as Node)) {
                setOpen(false);
            }
        };
        document.addEventListener('mousedown', close);
        document.addEventListener('keydown', close);
        return () => {
            document.removeEventListener('mousedown', close);
            document.removeEventListener('keydown', close);
        };
    }, [open]);

    return (
        <div
            className='mmd-menu'
            ref={ref}
        >
            <button
                className='mmd-menu-button'
                aria-label='More actions'
                aria-haspopup='menu'
                aria-expanded={open}
                onClick={() => setOpen((o) => !o)}
            >{'⋯'}</button>
            {open && (
                <ul
                    className='mmd-menu-list'
                    role='menu'
                >
                    {items.map(([label, action, danger]) => (
                        <li key={label}>
                            <button
                                role='menuitem'
                                className={danger ? 'mmd-menu-item mmd-menu-item--danger' : 'mmd-menu-item'}
                                onClick={() => {
                                    setOpen(false);
                                    action();
                                }}
                            >{label}</button>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}
