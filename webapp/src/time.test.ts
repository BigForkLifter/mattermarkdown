import {relativeTime} from 'time';

const MINUTE = 60 * 1000;

test('formats relative times', () => {
    const now = Date.UTC(2026, 0, 1);
    expect(relativeTime(now - 10000, now)).toBe('just now');
    expect(relativeTime(now - (5 * MINUTE), now)).toBe('5 minutes ago');
    expect(relativeTime(now - (26 * 60 * MINUTE), now)).toBe('yesterday');
});
