import {markdown} from '@codemirror/lang-markdown';
import {Compartment} from '@codemirror/state';
import {EditorView, keymap} from '@codemirror/view';
import {basicSetup} from 'codemirror';
import React, {useEffect, useRef} from 'react';
import {normalizeLineBreaks} from 'text';

type Props = {
    initialValue: string;
    readOnly?: boolean;
    onChange: (value: string) => void;
    onSave: () => void;
};

const theme = EditorView.theme({
    '&': {height: '100%', backgroundColor: 'var(--center-channel-bg)', color: 'var(--center-channel-color)'},
    '.cm-scroller': {fontFamily: 'Menlo, Consolas, "Liberation Mono", monospace', fontSize: '13.5px', lineHeight: '1.6'},
    '.cm-content': {caretColor: 'var(--center-channel-color)', padding: '16px 0'},

    // CodeMirror draws its own cursor, 1px black by default: faint on light themes, invisible on dark.
    '.cm-cursor, .cm-dropCursor': {borderLeftColor: 'var(--center-channel-color)', borderLeftWidth: '2px'},
    '.cm-gutters': {backgroundColor: 'transparent', color: 'rgba(var(--center-channel-color-rgb), 0.4)', border: 'none'},
    '.cm-activeLine, .cm-activeLineGutter': {backgroundColor: 'rgba(var(--center-channel-color-rgb), 0.04)'},
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {backgroundColor: 'rgba(var(--button-bg-rgb), 0.2)'},
    '&.cm-focused': {outline: 'none'},
});

/**
 * A CodeMirror Markdown editor. It is uncontrolled: initialValue is read once, so remount it
 * (change its key) to load different text.
 */
export default function MarkdownEditor({initialValue, readOnly = false, onChange, onSave}: Props) {
    const host = useRef<HTMLDivElement>(null);
    const view = useRef<EditorView>();
    const editable = useRef(new Compartment());

    // Keep the latest callbacks without rebuilding the editor on every render.
    const callbacks = useRef({onChange, onSave});
    callbacks.current = {onChange, onSave};

    useEffect(() => {
        view.current = new EditorView({
            parent: host.current!,
            doc: normalizeLineBreaks(initialValue),
            extensions: [
                keymap.of([{
                    key: 'Mod-s',
                    preventDefault: true,
                    run: () => {
                        callbacks.current.onSave();
                        return true;
                    },
                }]),
                basicSetup,
                markdown(),
                EditorView.lineWrapping,
                EditorView.clipboardInputFilter.of(normalizeLineBreaks),
                editable.current.of(EditorView.editable.of(!readOnly)),
                theme,
                EditorView.updateListener.of((update) => {
                    if (update.docChanged) {
                        callbacks.current.onChange(update.state.doc.toString());
                    }
                }),
            ],
        });
        view.current.focus();
        return () => view.current?.destroy();

        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // Toggle editing in place so the text survives, e.g. when the edit lock is lost.
    useEffect(() => {
        view.current?.dispatch({effects: editable.current.reconfigure(EditorView.editable.of(!readOnly))});
    }, [readOnly]);

    return (
        <div
            className='mmd-codemirror'
            ref={host}
        />
    );
}
