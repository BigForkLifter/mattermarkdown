import {ApiError, deleteDoc, getDoc, getRevision, listFolders, listRevisions, lockDoc, saveDoc, unlockDoc} from 'client';
import type {Doc, Revision} from 'client';
import {folderPath, indexFolders} from 'folders';
import {closeDoc, selectLastEvent, selectOpenDoc} from 'plugin_state';
import React, {useCallback, useDeferredValue, useEffect, useRef, useState} from 'react';
import {useDispatch, useSelector} from 'react-redux';
import {useHistory} from 'react-router-dom';
import {relativeTime} from 'time';

import {getCurrentUserId} from 'mattermost-redux/selectors/entities/common';

import {DocIcon, MarkdownView, Username} from './common';
import MarkdownEditor from './markdown_editor';
import SharePanel from './share_panel';

/** The server lock lasts 90s; renewing every 30s survives a missed heartbeat. */
const HEARTBEAT_MS = 30 * 1000;

type Mode = 'view' | 'edit' | 'history';
type Draft = {title: string; content: string};

/** Full-screen document viewer and editor, shown while a document is open. */
export default function DocEditor() {
    const open = useSelector(selectOpenDoc);
    if (!open) {
        return null;
    }
    return (
        <DocEditorInner
            key={open.id}
            docId={open.id}
            startEditing={open.startEditing}
        />
    );
}

function DocEditorInner({docId, startEditing}: {docId: string; startEditing: boolean}) {
    const dispatch = useDispatch();
    const currentUserId = useSelector(getCurrentUserId);
    const lastEvent = useSelector(selectLastEvent);

    const [doc, setDoc] = useState<Doc | null>(null);
    const [loadError, setLoadError] = useState('');
    const [notice, setNotice] = useState('');
    const [mode, setMode] = useState<Mode>('view');
    const [draft, setDraft] = useState<Draft | null>(null);
    const [editorKey, setEditorKey] = useState(0);
    const [lockLost, setLockLost] = useState(false);
    const [busy, setBusy] = useState(false);
    const [showPreview, setShowPreview] = useState(true);
    const [savedAt, setSavedAt] = useState(0);
    const [deleted, setDeleted] = useState(false);

    const editing = mode === 'edit' && draft !== null;
    const dirty = editing && doc !== null && (draft.title !== doc.title || draft.content !== (doc.content ?? ''));
    const lockedByOther = Boolean(doc?.lock_user_id) && doc?.lock_user_id !== currentUserId;
    const previewText = useDeferredValue(draft?.content ?? '');

    const history = useHistory();

    // Links to this Mattermost (channels, permalinks, other documents) navigate in the app.
    const followLink = (path: string) => {
        if (dirty && !window.confirm('Leave and discard your unsaved changes?')) { // eslint-disable-line no-alert
            return;
        }
        dispatch(closeDoc());
        history.push(path);
    };
    const [sharing, setSharing] = useState(false);
    const stopSharing = useCallback(() => setSharing(false), []);
    const [folderLabel, setFolderLabel] = useState('');

    // Show where the document is filed, e.g. "Runbooks / Pager".
    useEffect(() => {
        if (!doc?.folder_id) {
            setFolderLabel('');
            return;
        }
        listFolders(doc.channel_id).then((folders) => {
            setFolderLabel(folderPath(indexFolders(folders), doc.folder_id).map((f) => f.name).join(' / '));
        }).catch(() => setFolderLabel(''));
    }, [doc?.channel_id, doc?.folder_id]);

    const reload = useCallback(async () => {
        try {
            setDoc(await getDoc(docId));
            setLoadError('');
        } catch (e) {
            if (e instanceof ApiError && e.status === 404) {
                setDeleted(true);
            } else {
                setLoadError((e as Error).message);
            }
        }
    }, [docId]);

    const beginEdit = useCallback(async (initial?: Draft) => {
        setBusy(true);
        setNotice('');
        try {
            const locked = await lockDoc(docId);
            setDoc(locked);
            setDraft(initial ?? {title: locked.title, content: locked.content ?? ''});
            setEditorKey((k) => k + 1);
            setLockLost(false);
            setMode('edit');
        } catch (e) {
            if (e instanceof ApiError && e.id === 'locked' && e.doc) {
                setDoc(e.doc);
            } else {
                setNotice((e as Error).message);
            }
        } finally {
            setBusy(false);
        }
    }, [docId]);

    // Load, then go straight to editing for a document that was just created.
    useEffect(() => {
        reload().then(() => {
            if (startEditing) {
                beginEdit();
            }
        });

        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // Release the lock if the editor goes away mid-edit, e.g. when another document is opened.
    const holdsLock = useRef(false);
    holdsLock.current = editing && !lockLost;
    useEffect(() => () => {
        if (holdsLock.current) {
            unlockDoc(docId).catch(() => {});
        }
    }, [docId]);

    // Keep the lock alive while editing. A 409 means it lapsed and someone else took it.
    useEffect(() => {
        if (!editing) {
            return undefined;
        }
        const timer = window.setInterval(async () => {
            try {
                const renewed = await lockDoc(docId);
                setLockLost(false);
                setDoc((prev) => prev && {...prev, lock_user_id: renewed.lock_user_id, lock_expires_at: renewed.lock_expires_at});
            } catch (e) {
                if (e instanceof ApiError && e.id === 'locked') {
                    setLockLost(true);
                }
            }
        }, HEARTBEAT_MS);
        return () => window.clearInterval(timer);
    }, [editing, docId]);

    // Follow other people's changes while reading.
    useEffect(() => {
        if (!lastEvent || lastEvent.doc_id !== docId) {
            return;
        }
        if (lastEvent.action === 'deleted') {
            setDeleted(true);
        } else if (mode !== 'edit') {
            reload();
        }

        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [lastEvent?.seq]);

    // A lock that lapses without being released sends no event, so poll while someone else holds it.
    useEffect(() => {
        if (editing || !lockedByOther) {
            return undefined;
        }
        const timer = window.setInterval(reload, HEARTBEAT_MS);
        return () => window.clearInterval(timer);
    }, [editing, lockedByOther, reload]);

    useEffect(() => {
        if (!dirty) {
            return undefined;
        }
        const warn = (e: BeforeUnloadEvent) => {
            e.preventDefault();
            e.returnValue = '';
        };
        window.addEventListener('beforeunload', warn);
        return () => window.removeEventListener('beforeunload', warn);
    }, [dirty]);

    const save = async (): Promise<boolean> => {
        if (!doc || !draft || busy) {
            return false;
        }
        if (lockLost) {
            setNotice('You no longer hold the edit lock, so this can\'t be saved. Copy your changes somewhere safe.');
            return false;
        }
        setBusy(true);
        try {
            const saved = await saveDoc(docId, doc.version, draft.title, draft.content);
            setDoc(saved);
            setDraft({title: saved.title, content: saved.content ?? ''});
            setSavedAt(Date.now());
            setNotice('');
            return true;
        } catch (e) {
            if (e instanceof ApiError && e.id === 'locked') {
                setLockLost(true);
            } else if (e instanceof ApiError && e.id === 'version_conflict') {
                setNotice('Someone saved a newer version while you were editing. Copy your changes, cancel, and edit again.');
            } else {
                setNotice((e as Error).message);
            }
            return false;
        } finally {
            setBusy(false);
        }
    };

    const stopEditing = async () => {
        setMode('view');
        setDraft(null);
        setLockLost(false);
        await unlockDoc(docId).catch(() => {});
        await reload();
    };

    const done = async () => {
        if (dirty && !(await save())) {
            return;
        }
        await stopEditing();
    };

    const cancel = async () => {
        if (dirty && !window.confirm('Discard your unsaved changes?')) { // eslint-disable-line no-alert
            return;
        }
        setNotice('');
        await stopEditing();
    };

    const close = () => {
        if (dirty && !window.confirm('Close and discard your unsaved changes?')) { // eslint-disable-line no-alert
            return;
        }

        // The unmount effect releases the lock.
        dispatch(closeDoc());
    };

    const remove = async () => {
        if (!doc || !window.confirm(`Delete "${doc.title}"? Its history is kept on the server, but it disappears for everyone.`)) { // eslint-disable-line no-alert
            return;
        }
        try {
            await deleteDoc(docId);
            dispatch(closeDoc());
        } catch (e) {
            setNotice(e instanceof ApiError && e.status === 403 ? 'Only the document\'s creator or a channel admin can delete it.' : (e as Error).message);
        }
    };

    // Listen on the window: focus often isn't inside the overlay, e.g. after a button disappears.
    // Not while editing, where Escape belongs to the editor and closing could lose work, nor while
    // the share panel is open, which closes itself on Escape.
    useEffect(() => {
        if (mode === 'edit' || sharing) {
            return undefined;
        }
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.stopPropagation();
                dispatch(closeDoc());
            }
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => window.removeEventListener('keydown', onKeyDown, true);
    }, [mode, sharing, dispatch]);

    let body: React.ReactNode;
    if (deleted) {
        body = <div className='mmd-message'>{'This document was deleted, or you no longer have access to it.'}</div>;
    } else if (loadError) {
        body = <div className='mmd-message mmd-error'>{loadError}</div>;
    } else if (!doc) {
        body = <div className='mmd-message'>{'Loading…'}</div>;
    } else if (editing) {
        body = (
            <div className={showPreview ? 'mmd-split' : 'mmd-split mmd-split--single'}>
                <div className='mmd-pane mmd-pane--editor'>
                    <MarkdownEditor
                        key={editorKey}
                        initialValue={draft.content}
                        onChange={(content) => setDraft((d) => d && {...d, content})}
                        onSave={save}
                    />
                </div>
                {showPreview && (
                    <div className='mmd-pane mmd-pane--preview'>
                        <MarkdownView
                            text={previewText}
                            onInternalLink={followLink}
                        />
                    </div>
                )}
            </div>
        );
    } else if (mode === 'history') {
        body = (
            <History
                doc={doc}
                onRestore={(rev) => beginEdit({title: rev.title, content: rev.content ?? ''})}
                onInternalLink={followLink}
            />
        );
    } else {
        body = (
            <div className='mmd-page'>
                <MarkdownView
                    text={doc.content ?? ''}
                    onInternalLink={followLink}
                />
            </div>
        );
    }

    let saveStatus = '';
    if (editing) {
        if (dirty) {
            saveStatus = 'Unsaved changes';
        } else if (savedAt) {
            saveStatus = 'Saved';
        }
    }

    return (
        <div
            className='mmd-overlay'
            role='dialog'
            aria-modal='true'
            aria-label={doc?.title ?? 'Document'}
        >
            <header className='mmd-header'>
                <span className='mmd-header-icon'><DocIcon size={20}/></span>
                <div className='mmd-header-titles'>
                    {editing ? (
                        <input
                            className='mmd-title-input'
                            value={draft.title}
                            maxLength={256}
                            aria-label='Title'
                            onChange={(e) => {
                                const title = e.target.value;
                                setDraft((d) => d && {...d, title});
                            }}
                        />
                    ) : (
                        <h2 className='mmd-title'>{doc?.title ?? ''}</h2>
                    )}
                    {doc && (
                        <div className='mmd-meta'>
                            {folderLabel && <span className='mmd-folder-label'>{`${folderLabel} · `}</span>}
                            {`Version ${doc.version} · updated ${relativeTime(doc.updated_at)} by `}
                            <Username userId={doc.updated_by}/>
                            {saveStatus && <span className='mmd-save-status'>{` · ${saveStatus}`}</span>}
                        </div>
                    )}
                </div>
                <div className='mmd-actions'>
                    {doc && !deleted && mode === 'view' && (
                        <>
                            <button
                                className='btn btn-tertiary btn-sm'
                                title='Copy a link or add it to the channel bookmarks'
                                onClick={() => setSharing(true)}
                            >{'Share…'}</button>
                            <button
                                className='btn btn-tertiary btn-sm'
                                onClick={() => setMode('history')}
                            >{'History'}</button>
                            <button
                                className='btn btn-tertiary btn-sm'
                                onClick={remove}
                            >{'Delete'}</button>
                            <button
                                className='btn btn-primary btn-sm'
                                disabled={busy || lockedByOther}
                                onClick={() => beginEdit()}
                            >{'Edit'}</button>
                        </>
                    )}
                    {editing && (
                        <>
                            <button
                                className='btn btn-tertiary btn-sm'
                                onClick={() => setShowPreview((v) => !v)}
                            >{showPreview ? 'Hide preview' : 'Show preview'}</button>
                            <button
                                className='btn btn-tertiary btn-sm'
                                onClick={cancel}
                            >{'Cancel'}</button>
                            <button
                                className='btn btn-tertiary btn-sm'
                                disabled={busy || !dirty || lockLost}
                                onClick={save}
                                title='Save (Ctrl+S)'
                            >{'Save'}</button>
                            <button
                                className='btn btn-primary btn-sm'
                                disabled={busy || lockLost}
                                onClick={done}
                            >{'Done'}</button>
                        </>
                    )}
                    {mode === 'history' && (
                        <button
                            className='btn btn-tertiary btn-sm'
                            onClick={() => setMode('view')}
                        >{'Back to document'}</button>
                    )}
                    <button
                        className='mmd-close'
                        aria-label='Close'
                        onClick={close}
                    >{'×'}</button>
                </div>
            </header>
            {lockedByOther && !editing && !deleted && (
                <div className='mmd-banner'>
                    <Username userId={doc!.lock_user_id}/>
                    {' is editing this document. You can read it now and edit once they\'re done.'}
                </div>
            )}
            {lockLost && (
                <div className='mmd-banner mmd-banner--error'>
                    {'Your edit lock expired and someone else is editing now. Copy your changes before closing; they can\'t be saved.'}
                </div>
            )}
            {notice && <div className='mmd-banner mmd-banner--error'>{notice}</div>}
            <main className='mmd-body'>{body}</main>
            {sharing && doc && (
                <SharePanel
                    target={{kind: 'doc', id: doc.id, channelId: doc.channel_id, name: doc.title}}
                    onClose={stopSharing}
                />
            )}
        </div>
    );
}

type HistoryProps = {
    doc: Doc;
    onRestore: (rev: Revision) => void;
    onInternalLink: (path: string) => void;
};

function History({doc, onRestore, onInternalLink}: HistoryProps) {
    const [revisions, setRevisions] = useState<Revision[] | null>(null);
    const [selected, setSelected] = useState<Revision | null>(null);
    const [error, setError] = useState('');

    const select = async (version: number) => {
        try {
            setSelected(await getRevision(doc.id, version));
        } catch (e) {
            setError((e as Error).message);
        }
    };

    useEffect(() => {
        listRevisions(doc.id).then((revs) => {
            setRevisions(revs);
            if (revs.length) {
                select(revs[0].version);
            }
        }).catch((e) => setError(e.message));

        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [doc.id, doc.version]);

    if (error) {
        return <div className='mmd-message mmd-error'>{error}</div>;
    }
    if (!revisions) {
        return <div className='mmd-message'>{'Loading…'}</div>;
    }

    return (
        <div className='mmd-history'>
            <ul className='mmd-rev-list'>
                {revisions.map((rev) => (
                    <li key={rev.version}>
                        <button
                            className={rev.version === selected?.version ? 'mmd-rev mmd-rev--selected' : 'mmd-rev'}
                            onClick={() => select(rev.version)}
                        >
                            <span className='mmd-rev-version'>
                                {`Version ${rev.version}`}
                                {rev.version === doc.version && <span className='mmd-badge'>{'current'}</span>}
                            </span>
                            <span className='mmd-rev-meta'>
                                {`${relativeTime(rev.created_at)} · `}
                                <Username userId={rev.created_by}/>
                            </span>
                        </button>
                    </li>
                ))}
            </ul>
            <div className='mmd-rev-view'>
                {selected && (
                    <>
                        <div className='mmd-rev-toolbar'>
                            <strong>{selected.title}</strong>
                            {selected.version !== doc.version && (
                                <button
                                    className='btn btn-primary btn-sm'
                                    disabled={Boolean(doc.lock_user_id)}
                                    title={doc.lock_user_id ? 'Someone is editing this document' : ''}
                                    onClick={() => onRestore(selected)}
                                >{'Restore this version'}</button>
                            )}
                        </div>
                        <MarkdownView
                            text={selected.content ?? ''}
                            onInternalLink={onInternalLink}
                        />
                    </>
                )}
            </div>
        </div>
    );
}
