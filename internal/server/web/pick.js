// Resolve the word under a dblclick without relying on browser text selection.
// Android WebView emits dblclick for a double tap but can leave Selection empty.
(function () {
  "use strict";
  window.wuWordAt = function (event, articleRoot) {
    if (!Intl.Segmenter) return "";
    const doc = articleRoot.ownerDocument || document;
    let node, offset;
    const pos = doc.caretPositionFromPoint?.(event.clientX, event.clientY,
      articleRoot instanceof ShadowRoot ? {shadowRoots: [articleRoot]} : undefined);
    if (pos) { node = pos.offsetNode; offset = pos.offset; }
    else {
      const range = doc.caretRangeFromPoint?.(event.clientX, event.clientY);
      if (range) { node = range.startContainer; offset = range.startOffset; }
    }
    if (node?.nodeType !== Node.TEXT_NODE || !articleRoot.contains(node)) return "";
    let block = node.parentElement;
    while (block && block !== articleRoot) {
      if (/^(block|flow-root|list-item|table-cell|flex|grid)$/.test(
        getComputedStyle(block).display)) break;
      block = block.parentElement;
    }
    block ||= articleRoot;
    let text = "", at = -1;
    const atNodeEnd = offset === node.textContent.length;
    const append = s => { text += s; };
    const walk = el => {
      if (text.length > 20000) return;
      if (el.nodeType === Node.TEXT_NODE) {
        if (el === node) at = text.length + offset;
        append(el.textContent);
        return;
      }
      if (el.nodeType !== Node.ELEMENT_NODE && el !== articleRoot) return;
      if (/^(SCRIPT|STYLE|NOSCRIPT|TEMPLATE|INPUT|TEXTAREA|SELECT)$/.test(el.tagName))
        return;
      if (el.tagName === "BR") { append(" "); return; }
      const boundary = el !== block && (/^(A|BUTTON)$/.test(el.tagName) ||
        /^(block|flow-root|list-item|table-cell|flex|grid)$/.test(
          getComputedStyle(el).display));
      if (boundary) append(" ");
      for (const child of el.childNodes) walk(child);
      if (boundary) append(" ");
    };
    walk(block);
    if (at < 0 || !text) return "";
    const lang = node.parentElement?.closest("[lang]")?.lang || doc.documentElement.lang;
    const parts = new Intl.Segmenter(lang || undefined, {granularity: "word"}).segment(text);
    let part = parts.containing(Math.min(at, text.length - 1));
    if (!part?.isWordLike && at > 0 && atNodeEnd)
      part = parts.containing(at - 1);
    return part?.isWordLike ? part.segment : "";
  };
})();
