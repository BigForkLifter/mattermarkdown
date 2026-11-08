import {renderMarkdown} from 'markdown';

// Drop the newlines markdown-it adds after block tags and <br>, keeping all other whitespace.
const html = (text: string) => renderMarkdown(text).replace(/(<\/?(?:p|ul|ol|li|h\d|blockquote|table|thead|tbody|tr|th|td|pre)\b[^>]*>|<hr>|<br>)\n/g, '$1').trim();

describe('real pastes that rendered wrongly with the Mattermost post renderer', () => {
    test('a "*" list after a numbered line stays a bullet list', () => {
        const out = html('2. Market Research & Target Audience\n\n* Demographics: profiles. \n* Style Benchmarking: styles. \n');
        expect(out).toBe('<ol start="2"><li>Market Research &amp; Target Audience</li></ol>' +
            '<ul><li>Demographics: profiles.</li><li>Style Benchmarking: styles.</li></ul>');
    });

    test('a line holding only a space ends the paragraph; line breaks show', () => {
        const out = html('the site.\n \n**Art direction**\n They won’t commit');
        expect(out).toBe('<p>the site.</p><p><strong>Art direction</strong><br>They won’t commit</p>');
    });

    test('a line holding only non-breaking spaces ends the paragraph too', () => {
        expect(html('a\n  \nb')).toBe('<p>a</p><p>b</p>');
    });
});

describe('CommonMark behavior', () => {
    test('changing the bullet character starts a new list', () => {
        expect(html('- a\n- b\n+ c\n')).toBe('<ul><li>a</li><li>b</li></ul><ul><li>c</li></ul>');
    });

    test('ordered lists keep their start number', () => {
        expect(html('3. three\n4. four\n')).toBe('<ol start="3"><li>three</li><li>four</li></ol>');
    });

    test('nested lists', () => {
        expect(html('- a\n  - b\n    1. c\n')).toBe('<ul><li>a\n<ul><li>b\n<ol><li>c</li></ol></li></ul></li></ul>');
    });

    test('a blank line between items makes a loose list', () => {
        expect(html('- a\n\n- b\n')).toBe('<ul><li><p>a</p></li><li><p>b</p></li></ul>');
    });

    test('headings, emphasis, inline code, blockquotes, rules', () => {
        expect(html('# H1\n### H3\n*em* **strong** ~~gone~~ `code`\n\n> quote\n\n---\n')).toBe(
            '<h1>H1</h1><h3>H3</h3><p><em>em</em> <strong>strong</strong> <s>gone</s> <code>code</code></p>' +
            '<blockquote><p>quote</p></blockquote><hr>');
    });

    test('fenced code keeps its text and language, unformatted', () => {
        expect(html('```js\nconst a = 1;\n\n**not bold**\n```\n')).toBe(
            '<pre><code class="language-js">const a = 1;\n\n**not bold**\n</code></pre>');
    });

    test('a numbered line needs a blank line or a list to start a list mid-paragraph', () => {
        // CommonMark: only "1." may interrupt a paragraph, so "2019. was a year" stays text.
        expect(html('In\n2019. was a year')).toBe('<p>In<br>2019. was a year</p>');
    });
});

describe('GitHub extensions', () => {
    test('tables with alignment', () => {
        expect(html('| A | B |\n|:--|--:|\n| 1 | 2 |\n')).toBe(
            '<table><thead><tr><th style="text-align:left">A</th><th style="text-align:right">B</th></tr></thead>' +
            '<tbody><tr><td style="text-align:left">1</td><td style="text-align:right">2</td></tr></tbody></table>');
    });

    test('task lists render read-only checkboxes', () => {
        expect(html('- [ ] todo\n- [x] done\n- [link](https://x.test) normal\n')).toBe(
            '<ul><li class="mmd-task-item"><input type="checkbox" class="mmd-task-checkbox" disabled> todo</li>' +
            '<li class="mmd-task-item"><input type="checkbox" class="mmd-task-checkbox" disabled checked> done</li>' +
            '<li><a href="https://x.test" target="_blank" rel="noopener noreferrer">link</a> normal</li></ul>');
    });

    test('bare URLs become links', () => {
        expect(html('see https://example.com/a')).toBe('<p>see <a href="https://example.com/a" target="_blank" rel="noopener noreferrer">https://example.com/a</a></p>');
    });
});

describe('safety', () => {
    test('raw HTML is shown as text', () => {
        expect(html('<script>alert(1)</script>\n\n<b onclick="x">hi</b>')).toBe(
            '<p>&lt;script&gt;alert(1)&lt;/script&gt;</p><p>&lt;b onclick=&quot;x&quot;&gt;hi&lt;/b&gt;</p>');
    });

    // eslint-disable-next-line no-script-url
    test('javascript: links are not linked', () => {
        expect(html('[x](javascript:alert(1))')).toBe('<p>[x](javascript:alert(1))</p>');
    });
});
