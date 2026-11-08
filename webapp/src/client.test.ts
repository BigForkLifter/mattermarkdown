import {ApiError, lockDoc, saveDoc} from 'client';

// Client4.getOptions checks for FormData bodies; Jest's node environment has no FormData.
(global as any).FormData ??= class {};

const respond = (status: number, body: unknown) => {
    global.fetch = jest.fn().mockResolvedValue({
        ok: status < 300,
        status,
        json: () => Promise.resolve(body),
    }) as any;
};

test('a refused lock carries the document naming the holder', async () => {
    respond(409, {id: 'doc1', lock_user_id: 'alice', title: 'Runbook'});
    const err = await lockDoc('doc1').catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.id).toBe('locked');
    expect(err.doc.lock_user_id).toBe('alice');
});

test('save conflicts keep the server error id', async () => {
    respond(409, {error: 'document was changed since it was loaded', id: 'version_conflict'});
    const err = await saveDoc('doc1', 1, 't', 'c').catch((e) => e);
    expect(err.id).toBe('version_conflict');
    expect(err.doc).toBeUndefined();
});

test('sends JSON with the CSRF-aware Mattermost options', async () => {
    respond(200, {id: 'doc1'});
    await saveDoc('doc1', 3, 'Title', 'Body');
    const [url, options] = (global.fetch as jest.Mock).mock.calls[0];
    expect(url).toBe('/plugins/com.otules.mattermarkdown/api/v1/docs/doc1');
    expect(options.method).toBe('PUT');
    expect(JSON.parse(options.body)).toEqual({base_version: 3, title: 'Title', content: 'Body'});
    expect(options.headers['X-Requested-With']).toBe('XMLHttpRequest');
});
