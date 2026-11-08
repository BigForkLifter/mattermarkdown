import type {Folder} from 'client';

export type FolderIndex = {
    byId: Map<string, Folder>;

    /** Child folders of each parent ('' for the top level), in the server's manual order. */
    children: Map<string, Folder[]>;
};

export function indexFolders(folders: Folder[]): FolderIndex {
    const byId = new Map(folders.map((f) => [f.id, f]));
    const children = new Map<string, Folder[]>();
    for (const folder of folders) {
        // A folder whose parent is gone shows at the top level rather than disappearing.
        const parent = byId.has(folder.parent_id) ? folder.parent_id : '';
        children.set(parent, [...(children.get(parent) ?? []), folder]);
    }
    return {byId, children};
}

/**
 * The list with the item at `from` moved to slot `to` of the original list, for optimistic drag
 * reordering; a slot past the end drops the item last.
 */
export function reordered<T>(list: T[], from: number, to: number): T[] {
    const copy = [...list];
    const [item] = copy.splice(from, 1);
    copy.splice(from < to ? to - 1 : to, 0, item);
    return copy;
}

/** The folders from the top level down to folderId, inclusive. Empty for the top level or an unknown folder. */
export function folderPath(index: FolderIndex, folderId: string): Folder[] {
    const path: Folder[] = [];
    const seen = new Set<string>();
    for (let f = index.byId.get(folderId); f && !seen.has(f.id); f = index.byId.get(f.parent_id)) {
        seen.add(f.id);
        path.unshift(f);
    }
    return path;
}

/**
 * Where a folder or document can be moved, as [id, label] pairs with '' for the top level.
 * Moving a folder excludes the folder itself and everything under it.
 */
export function moveTargets(index: FolderIndex, movingFolderId = ''): Array<[string, string]> {
    const targets: Array<[string, string]> = [['', 'Top level']];
    const walk = (parentId: string, prefix: string) => {
        for (const folder of index.children.get(parentId) ?? []) {
            if (folder.id === movingFolderId) {
                continue;
            }
            const label = prefix ? `${prefix} / ${folder.name}` : folder.name;
            targets.push([folder.id, label]);
            walk(folder.id, label);
        }
    };
    walk('', '');
    return targets;
}
