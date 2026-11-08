import MarkdownIt from 'markdown-it';
import {prepareForRender} from 'text';

/**
 * Documents are rendered with markdown-it, which follows CommonMark, plus GitHub's tables,
 * strikethrough, autolinks and task lists. Mattermost's own post renderer (PostUtils.formatText)
 * isn't CommonMark: it merges a "*" list into a preceding "1." list, for one. See agent_docs/findings.md.
 *
 * Deliberate differences from strict CommonMark:
 * - breaks: a single newline is a line break, as in GitHub comments and Mattermost posts.
 * - html: raw HTML is shown as text, never rendered, so documents can't inject markup.
 */
const md = new MarkdownIt({html: false, linkify: true, breaks: true});

// GitHub task lists: "- [ ] item" and "- [x] item" render as read-only checkboxes.
md.core.ruler.after('inline', 'task_lists', (state) => {
    const tokens = state.tokens;
    for (let i = 2; i < tokens.length; i++) {
        const inline = tokens[i];
        if (inline.type !== 'inline' || tokens[i - 1].type !== 'paragraph_open' || tokens[i - 2].type !== 'list_item_open') {
            continue;
        }
        const first = inline.children?.[0];
        const match = first?.type === 'text' ? (/^\[([ xX])\]\s+/).exec(first.content) : null;
        if (!first || !match) {
            continue;
        }
        first.content = first.content.slice(match[0].length);
        const checkbox = new state.Token('html_inline', '', 0);
        checkbox.content = `<input type="checkbox" class="mmd-task-checkbox" disabled${match[1] === ' ' ? '' : ' checked'}> `;
        inline.children!.unshift(checkbox);
        tokens[i - 2].attrJoin('class', 'mmd-task-item');
    }
});

// Links to other sites open in a new tab; links within this Mattermost stay in the app (see MarkdownView).
const defaultLinkOpen = md.renderer.rules.link_open ?? ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));
md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
    const href = tokens[idx].attrGet('href') ?? '';
    if ((/^[a-z][a-z0-9+.-]*:/i).test(href) && !isSameOrigin(href)) {
        tokens[idx].attrSet('target', '_blank');
        tokens[idx].attrSet('rel', 'noopener noreferrer');
    }
    return defaultLinkOpen(tokens, idx, options, env, self);
};

function isSameOrigin(href: string): boolean {
    if (typeof window === 'undefined') {
        return false;
    }
    try {
        return new URL(href).origin === window.location.origin;
    } catch {
        return false;
    }
}

export function renderMarkdown(text: string): string {
    return md.render(prepareForRender(text));
}
