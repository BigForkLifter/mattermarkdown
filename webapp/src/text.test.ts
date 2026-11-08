import {normalizeLineBreaks, prepareForRender} from 'text';

test('turns every kind of line break into \\n', () => {
    expect(normalizeLineBreaks('a\u2028b\u2029c\r\nd\re\vf\u0085g')).toBe('a\nb\nc\nd\ne\nf\ng');
});

test('leaves ordinary text alone', () => {
    const text = '# Title\n\n- item\n\ttabbed';
    expect(normalizeLineBreaks(text)).toBe(text);
});

test('lines of only spaces become blank lines for rendering', () => {
    // From a real paste: the "blank" line held a space, so the heading merged into the paragraph.
    expect(prepareForRender('site.\n \n**Art direction**\n They')).toBe('site.\n\n**Art direction**\n They');
    expect(prepareForRender('a\n\u00a0\t\nb')).toBe('a\n\nb');
    expect(prepareForRender('  indented text stays')).toBe('  indented text stays');
});
