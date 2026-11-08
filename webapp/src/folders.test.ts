import type {Folder} from 'client';
import {folderPath, indexFolders, moveTargets, reordered} from 'folders';

const folder = (id: string, name: string, parentId = '', sortOrder = 0): Folder =>
    ({id, name, parent_id: parentId, sort_order: sortOrder, channel_id: 'c', created_by: 'u', created_at: 0});

const index = indexFolders([
    folder('rb', 'Runbooks'),
    folder('pg', 'pager', 'rb'),
    folder('db', 'Databases', 'rb'),
    folder('ar', 'Archive'),
    folder('orphan', 'Orphan', 'gone'),
]);

test('keeps the server order and lifts orphans to the top level', () => {
    expect(index.children.get('')?.map((f) => f.name)).toEqual(['Runbooks', 'Archive', 'Orphan']);
    expect(index.children.get('rb')?.map((f) => f.name)).toEqual(['pager', 'Databases']);
});

test('builds the path from the top level', () => {
    expect(folderPath(index, 'pg').map((f) => f.id)).toEqual(['rb', 'pg']);
    expect(folderPath(index, '')).toEqual([]);
    expect(folderPath(index, 'unknown')).toEqual([]);
});

test('survives a cycle in the data', () => {
    const cyclic = indexFolders([folder('a', 'A', 'b'), folder('b', 'B', 'a')]);
    expect(folderPath(cyclic, 'a').length).toBeLessThanOrEqual(2);
});

test('lists move targets with paths, excluding the moving folder and its subtree', () => {
    expect(moveTargets(index)).toEqual([
        ['', 'Top level'],
        ['rb', 'Runbooks'],
        ['pg', 'Runbooks / pager'],
        ['db', 'Runbooks / Databases'],
        ['ar', 'Archive'],
        ['orphan', 'Orphan'],
    ]);
    expect(moveTargets(index, 'rb').map(([id]) => id)).toEqual(['', 'ar', 'orphan']);
});

test('reordered moves one item to another slot', () => {
    expect(reordered(['a', 'b', 'c'], 0, 2)).toEqual(['b', 'a', 'c']);
    expect(reordered(['a', 'b', 'c'], 0, 3)).toEqual(['b', 'c', 'a']);
    expect(reordered(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b']);
    expect(reordered(['a', 'b', 'c'], 1, 1)).toEqual(['a', 'b', 'c']);
});
