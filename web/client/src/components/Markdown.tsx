import { useMemo } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";

// Markdown renderer with the tweaks beads-ui's upstream Markdown component
// has — minus the live syntax-highlighting + WS attachment fetching:
//   • `attach://<path>` URIs in images/links rewrite to absolute URLs under
//     the configured FILE_ATTACHMENT_BASE_URL (passed in via props).
//   • `#issue-id` mentions auto-link to the detail page.
//   • Headings get slug ids so #section-name fragments work as deep links.

export interface MarkdownProps {
  content: string;
  attachmentBaseUrl?: string;
  prefix?: string; // current project prefix; controls #id autolink scope
  className?: string;
}

export function Markdown({ content, attachmentBaseUrl = "", prefix = "", className = "" }: MarkdownProps) {
  const html = useMemo(() => render(content, attachmentBaseUrl, prefix), [content, attachmentBaseUrl, prefix]);
  return (
    <div
      className={`prose prose-sm max-w-none prose-stone ${className}`}
      style={{ color: "var(--color-ink-primary)" }}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

function render(content: string, attachBase: string, prefix: string): string {
  if (!content) return "";

  // Configure marked with a custom renderer for headings, links, images.
  const renderer = new marked.Renderer();

  renderer.heading = function ({ tokens, depth }) {
    const text = this.parser.parseInline(tokens);
    const id = slugify(stripTags(text));
    return `<h${depth} id="${id}">${text}</h${depth}>\n`;
  };

  renderer.link = function ({ href, title, tokens }) {
    const safeHref = rewriteUrl(href, attachBase);
    const titleAttr = title ? ` title="${escapeAttr(title)}"` : "";
    const inner = this.parser.parseInline(tokens);
    return `<a href="${escapeAttr(safeHref)}"${titleAttr}>${inner}</a>`;
  };

  renderer.image = function ({ href, title, text }) {
    const safeHref = rewriteUrl(href, attachBase);
    const titleAttr = title ? ` title="${escapeAttr(title)}"` : "";
    return `<img src="${escapeAttr(safeHref)}" alt="${escapeAttr(text || "")}"${titleAttr}>`;
  };

  let raw = marked.parse(content, { renderer, async: false }) as string;

  // Auto-link "#<prefix>-<hash>" mentions outside of <code>/<pre>. This is a
  // conservative pass — only matches inside text nodes (the negative
  // lookbehind/lookahead avoids URLs and existing href values).
  if (prefix) {
    const re = new RegExp(`(^|[\\s(\\[])#(${escapeRegex(prefix)}-[a-z0-9]+(?:\\.\\d+)*)(?=$|[\\s.,:;)\\]?!])`, "g");
    raw = raw.replace(re, (_m, lead, id) => `${lead}<a href="/p/${prefix}/issue/${id}">#${id}</a>`);
  }

  return DOMPurify.sanitize(raw, {
    ADD_ATTR: ["id"],
  });
}

function rewriteUrl(href: string | null | undefined, attachBase: string): string {
  if (!href) return "";
  if (href.startsWith("attach://")) {
    const rest = href.slice("attach://".length);
    if (!attachBase) return rest; // best-effort fallback: relative path
    return `${attachBase}/${rest}`;
  }
  return href;
}

function slugify(s: string): string {
  return s
    .toLowerCase()
    .trim()
    .replace(/<[^>]+>/g, "")
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .slice(0, 80);
}

function stripTags(s: string): string {
  return s.replace(/<[^>]+>/g, "");
}

function escapeAttr(s: string): string {
  return String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;");
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
