// A tiny, safe markdown renderer for the AI briefing. The model emits **bold**,
// "- " bullets and the occasional "#" heading, which render as literal symbols
// in plain text. We HTML-escape the input FIRST, then inject only our own known
// tags, so `{@html}` on the result carries no injection risk.

function esc(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// inline: escape, then bold / italic / code on the escaped text.
function inline(s: string): string {
  s = esc(s);
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/`([^`]+)`/g, "<code>$1</code>");
  // italic: single * or _ not part of a ** run
  s = s.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, "$1<em>$2</em>");
  s = s.replace(/(^|[^_])_([^_\n]+)_(?!_)/g, "$1<em>$2</em>");
  return s;
}

// renderBrief turns the briefing markdown into safe HTML (headings, bullets,
// paragraphs, inline emphasis). Consecutive bullets group into one <ul>.
export function renderBrief(md: string): string {
  const lines = (md || "").split(/\r?\n/);
  const out: string[] = [];
  let inList = false;
  const closeList = () => {
    if (inList) {
      out.push("</ul>");
      inList = false;
    }
  };
  for (const raw of lines) {
    const line = raw.trim();
    const bullet = line.match(/^[-*]\s+(.*)$/);
    if (bullet) {
      if (!inList) {
        out.push('<ul class="md-list">');
        inList = true;
      }
      out.push("<li>" + inline(bullet[1]) + "</li>");
      continue;
    }
    closeList();
    if (line === "") continue;
    const heading = line.match(/^#{1,6}\s+(.*)$/);
    if (heading) {
      out.push('<div class="md-h">' + inline(heading[1]) + "</div>");
      continue;
    }
    out.push("<p>" + inline(line) + "</p>");
  }
  closeList();
  return out.join("");
}
