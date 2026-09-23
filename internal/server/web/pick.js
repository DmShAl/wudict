/**
 * Copyright (C) 2026 glowinthedark
 *
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

// wudict word-at-point: the word under a double tap, for both article
// surfaces - index.html's shadow roots and frame.js's sandboxed iframes each
// load this file into their own window. It is only the fallback. A double
// click that selected something is looked up by its selection, phrase and
// all; this answers when there is none, which is what Android WebView leaves
// behind a double TAP - it selects on long-press only. Callers hand the result
// to the same tidying a selection gets.
//
// The browser's own word selection is not reachable without a selection, so
// this does what it does: the caret under the point, then Unicode word
// boundaries (Intl.Segmenter) - which is what makes a tap in Chinese or Thai
// text find a word and not a whole run of script.
(function () {
	"use strict";
	// a double tap on one of these is the element's, not a lookup
	var OWNED = "a,button,input,textarea,select,audio,video,[contenteditable]";
	var SKIP = /^(script|style|noscript|template|input|textarea|select)$/;
	var BLOCK = /^(block|flow-root|list-item|table|table-row|table-cell|table-caption|flex|grid)$/;

	function wordAt(e, root) {
		if (typeof Intl === "undefined" || !Intl.Segmenter) return "";
		var t = e.composedPath ? e.composedPath()[0] : e.target;
		if (t && t.nodeType !== 1) t = t.parentElement;
		if (t && t.closest && t.closest(OWNED)) return "";

		var doc = root.ownerDocument || document, view = doc.defaultView || window;
		var node = null, offset = 0, p;
		if (doc.caretPositionFromPoint) {
			// a shadow root must be named, or the caret stops at its host
			p = root.nodeType === 11
				? doc.caretPositionFromPoint(e.clientX, e.clientY, { shadowRoots: [root] })
				: doc.caretPositionFromPoint(e.clientX, e.clientY);
			if (p) { node = p.offsetNode; offset = p.offset; }
		} else if (doc.caretRangeFromPoint) {
			p = doc.caretRangeFromPoint(e.clientX, e.clientY);
			if (p) { node = p.startContainer; offset = p.startOffset; }
		}
		if (!node || node.nodeType !== 3 || !root.contains(node)) return "";

		var display = function (el) { return view.getComputedStyle(el).display; };
		// The text is the tapped node's whole block, never the node alone:
		// <b>un</b>likely is one word split across two nodes.
		var block = node.parentElement;
		while (block && block !== root && !BLOCK.test(display(block))) block = block.parentElement;
		if (!block) block = root; // top level of a shadow root: no element parent

		// Blocks and <br> inside it become spaces, or a table's cells would run
		// together into one word. Bounded around the tap, whatever the article's
		// size: before the tapped node only a short tail is kept, and the walk
		// ends a little past it - word boundaries are decided locally.
		var text = "", at = -1, done = false;
		var add = function (s) {
			text += s;
			if (at < 0) { if (text.length > 4096) text = text.slice(-256); }
			else if (text.length > at + 256) done = true;
		};
		var walk = function (n) {
			if (n.nodeType === 3) {
				if (n === node) at = text.length + offset;
				add(n.nodeValue);
				return;
			}
			var edge = false;
			if (n.nodeType === 1) {
				if (SKIP.test(n.localName)) return;
				if (n.localName === "br") { add(" "); return; }
				if (n !== block) {
					var d = display(n);
					if (d === "none") return;
					edge = BLOCK.test(d);
				}
			} else if (n.nodeType !== 11) return;
			if (edge) add(" ");
			for (var c = n.firstChild; c && !done; c = c.nextSibling) walk(c);
			if (edge) add(" ");
		};
		walk(block);
		if (at < 0 || !text) return "";

		var el = node.parentElement, tagged = el && el.closest("[lang]");
		var lang = (tagged ? tagged.lang : doc.documentElement.lang) || undefined, seg;
		// a dictionary's lang="en_US" is not a BCP 47 tag, and the constructor throws
		try { seg = new Intl.Segmenter(lang, { granularity: "word" }); }
		catch (_) { seg = new Intl.Segmenter(undefined, { granularity: "word" }); }
		var parts = seg.segment(text), i = Math.min(at, text.length - 1);
		var s = parts.containing(i);
		// A tap on a word's last letter puts the caret AFTER it, on the space.
		if ((!s || !s.isWordLike) && i > 0) s = parts.containing(i - 1);
		return s && s.isWordLike ? s.segment : "";
	}

	// Never throws: a failed guess is "no word", and the tap does nothing.
	window.wuWordAt = function (e, root) {
		try { return wordAt(e, root); } catch (_) { return ""; }
	};
})();
