import {closeDoc, docChanged, openDoc, reducer, selectLastEvent, selectOpenDoc} from 'plugin_state';

const asGlobal = (state: ReturnType<typeof reducer>) => ({'plugins-com.otules.mattermarkdown': state}) as any;

test('opens and closes a document', () => {
    let state = reducer(undefined, {type: 'init'});
    expect(selectOpenDoc(asGlobal(state))).toBeNull();

    state = reducer(state, openDoc('doc1', true));
    expect(selectOpenDoc(asGlobal(state))).toEqual({id: 'doc1', startEditing: true});

    state = reducer(state, closeDoc());
    expect(selectOpenDoc(asGlobal(state))).toBeNull();
});

test('numbers events so identical ones still register as changes', () => {
    const event = {doc_id: 'doc1', channel_id: 'ch1', action: 'updated'} as const;
    let state = reducer(undefined, docChanged(event));
    const first = selectLastEvent(asGlobal(state));
    state = reducer(state, docChanged(event));
    const second = selectLastEvent(asGlobal(state));

    expect(first).toEqual({...event, seq: 1});
    expect(second?.seq).toBe(2);
});
