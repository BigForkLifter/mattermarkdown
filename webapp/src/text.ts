// Line breaks other than "\n" that text copied from word processors, PDFs and web pages can carry.
// CodeMirror and Markdown treat only "\n" (plus "\r\n" and "\r") as line breaks, so these would
// otherwise collapse a pasted document onto one line. Keep in sync with normalizeLineBreaks in server/docs.go.
const LINE_BREAKS = /\r\n|[\r\v\u0085\u2028\u2029]/g;

export function normalizeLineBreaks(text: string): string {
    return text.replace(LINE_BREAKS, '\n');
}

// A line of only spaces looks blank but isn't one to Mattermost's Markdown renderer, so it doesn't
// end a paragraph. Non-breaking spaces (common when copying from web pages) count as spaces here.
const WHITESPACE_ONLY_LINE = /^[ \t\u00a0]+$/gm;

/** Prepares stored text for display. The stored text itself is left as written. */
export function prepareForRender(text: string): string {
    return normalizeLineBreaks(text).replace(WHITESPACE_ONLY_LINE, '');
}
