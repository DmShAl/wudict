/**
 * Copyright (C) 2026 glowinthedark
 *
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

// wudict read-aloud (D148): select text in an article, tap the speaker, hear
// it in the system voice. Fetched by index.html on the first selection inside
// an article and never before, so a reader who does not select text pays for
// nothing but two listeners, and one who switched it off not even those.
//
// Web Speech only. The Android shell has no working speechSynthesis in its
// WebView and injects one of its own over the platform TextToSpeech (Speech.java);
// this file cannot tell the two apart, which is the point.
(function () {
"use strict";
if (window.wuSpeak) return;

// Android's getMaxSpeechInputLength floor; a selection past it is a page, not
// a phrase, and reading it all would hold the device for minutes.
const MAX = 4000;
// One utterance per ~sentence. Chrome's network voices stop dead after ~15 s
// of one utterance and never fire `end`; short utterances also make Stop
// immediate everywhere.
const CHUNK = 160;
const VOICE_KEY = "wudict_voice_";

// ------------------------------------------------------------- language
// The article language alone is the wrong key: an en→ru article is Russian
// prose full of English examples. The SCRIPT of what was selected picks
// between the dictionary's two languages; a script neither of them writes in
// falls back to that script's most likely language.
const SCRIPTS = [
  ["Latn", /\p{Script=Latin}/u], ["Cyrl", /\p{Script=Cyrillic}/u],
  ["Kana", /[\p{Script=Hiragana}\p{Script=Katakana}]/u], ["Hani", /\p{Script=Han}/u],
  ["Hang", /\p{Script=Hangul}/u], ["Grek", /\p{Script=Greek}/u],
  ["Arab", /\p{Script=Arabic}/u], ["Hebr", /\p{Script=Hebrew}/u],
  ["Thai", /\p{Script=Thai}/u], ["Deva", /\p{Script=Devanagari}/u],
  ["Beng", /\p{Script=Bengali}/u], ["Guru", /\p{Script=Gurmukhi}/u],
  ["Gujr", /\p{Script=Gujarati}/u], ["Taml", /\p{Script=Tamil}/u],
  ["Telu", /\p{Script=Telugu}/u], ["Knda", /\p{Script=Kannada}/u],
  ["Mlym", /\p{Script=Malayalam}/u], ["Sinh", /\p{Script=Sinhala}/u],
  ["Geor", /\p{Script=Georgian}/u], ["Armn", /\p{Script=Armenian}/u],
  ["Ethi", /\p{Script=Ethiopic}/u], ["Khmr", /\p{Script=Khmer}/u],
  ["Laoo", /\p{Script=Lao}/u], ["Mymr", /\p{Script=Myanmar}/u],
  ["Tibt", /\p{Script=Tibetan}/u]
];
// language -> script, for every language not written in Latin. Absent = Latin.
const LANG_SCRIPT = {
  ru: "Cyrl", uk: "Cyrl", be: "Cyrl", bg: "Cyrl", sr: "Cyrl", mk: "Cyrl", kk: "Cyrl",
  ky: "Cyrl", tg: "Cyrl", mn: "Cyrl", ba: "Cyrl", tt: "Cyrl", cv: "Cyrl", ce: "Cyrl",
  os: "Cyrl", ab: "Cyrl", av: "Cyrl", cu: "Cyrl",
  el: "Grek", grc: "Grek",
  ar: "Arab", fa: "Arab", ur: "Arab", ps: "Arab", sd: "Arab", ug: "Arab",
  he: "Hebr", yi: "Hebr",
  zh: "Hani", yue: "Hani", ja: "Jpan", ko: "Hang",
  th: "Thai", hi: "Deva", mr: "Deva", ne: "Deva", sa: "Deva", bn: "Beng", as: "Beng",
  pa: "Guru", gu: "Gujr", ta: "Taml", te: "Telu", kn: "Knda", ml: "Mlym", si: "Sinh",
  ka: "Geor", hy: "Armn", am: "Ethi", ti: "Ethi", km: "Khmr", lo: "Laoo", my: "Mymr",
  bo: "Tibt", dz: "Tibt"
};
// script -> the language a selection in it most likely is, when neither of
// the dictionary's languages is written in it. Latin is decided separately.
const SCRIPT_LANG = {
  Cyrl: "ru", Grek: "el", Arab: "ar", Hebr: "he", Hani: "zh", Kana: "ja", Hang: "ko",
  Thai: "th", Deva: "hi", Beng: "bn", Guru: "pa", Gujr: "gu", Taml: "ta", Telu: "te",
  Knda: "kn", Mlym: "ml", Sinh: "si", Geor: "ka", Armn: "hy", Ethi: "am", Khmr: "km",
  Laoo: "lo", Mymr: "my", Tibt: "bo"
};
// Legacy and macro-language spellings that name the same voice.
const ALIAS = { iw: "he", in: "id", ji: "yi", nb: "no", fil: "tl", cmn: "zh" };

function norm(tag) {
  const b = String(tag || "").replace(/_/g, "-").split("-")[0].toLowerCase();
  return ALIAS[b] || b;
}

// The majority script of the selection's letters. Kana anywhere with Han
// around it is Japanese: Chinese is never written with kana.
function scriptOf(text) {
  const n = {};
  let seen = 0;
  for (const ch of text.slice(0, 400)) {
    if (ch < "A") continue;                                      // digits, space, ASCII punctuation
    if (ch <= "z") { if (/[A-Za-z]/.test(ch)) { n.Latn = (n.Latn || 0) + 1; seen++ } continue }
    for (const [sc, re] of SCRIPTS) if (re.test(ch)) { n[sc] = (n[sc] || 0) + 1; seen++; break }
  }
  if (!seen) return "";
  if (n.Kana) n.Kana += n.Hani || 0, n.Hani = 0;
  let best = "", max = 0;
  for (const sc in n) if (n[sc] > max) max = n[sc], best = sc;
  return best;
}

function fits(lang, sc) {
  const s = LANG_SCRIPT[lang] || "Latn";
  return s === "Jpan" ? sc === "Kana" || sc === "Hani" : s === sc;
}

function userLang() { return norm(navigator.language) || "en" }

// Article language first: it is what the reader is reading, and for a
// Latin/Latin pair (en→fr) script cannot tell the two apart, so the target
// wins and the other one is one tap away in the menu.
function decide(sc, a, h) {
  if (!sc) return a || h || userLang();
  for (const l of [a, h]) if (l && fits(l, sc)) return l;
  if (sc === "Latn") { const u = userLang(); return fits(u, "Latn") ? u : "en" }
  return SCRIPT_LANG[sc] || userLang();
}

let langNames = null;
function langName(code) {
  try {
    langNames = langNames || new Intl.DisplayNames([navigator.language, "en"], { type: "language" });
    return langNames.of(code) || code;
  } catch (_) { return code }
}

// --------------------------------------------------------------- voices
// Read at use, never captured: the Android shell replaces both objects after
// the page has loaded.
const S = () => window.speechSynthesis;
let voices = [], voicesDone = false, voicesStarted = false;

function readVoices() {
  try { voices = (S() && S().getVoices()) || [] } catch (_) { voices = [] }
  if (voices.length) voicesDone = true;
  if (ui && !ui.hidden) syncMore();
  if (menu && !menu.hidden) openMenu(menuNote);
}
// Voices load asynchronously in Chrome and on Android, synchronously in Safari
// and Firefox; none of them fires `voiceschanged` when there are none at all.
function startVoices() {
  if (voicesStarted || !S()) return;
  voicesStarted = true;
  try { S().addEventListener("voiceschanged", readVoices) } catch (_) { }
  readVoices();
  setTimeout(() => { voicesDone = true; readVoices() }, 3500);
}

// macOS ships joke voices, and the robotic Eloquence set in every language;
// neither is ever the right automatic answer. They stay available only where
// they are all a language has.
const NOVELTY = /^(Albert|Bad News|Bahh|Bells|Boing|Bubbles|Cellos|Good News|Jester|Organ|Pipe Organ|Superstar|Trinoids|Whisper|Wobble|Zarvox|Deranged|Hysterical|Eddy|Flo|Grandma|Grandpa|Reed|Rocko|Sandy|Shelley)\b/;

function voicesFor(L) {
  const all = voices.filter(v => norm(v.lang) === L);
  const good = all.filter(v => !NOVELTY.test(v.name || ""));
  return good.length ? good : all;
}

function storedVoice(L) {
  try { return localStorage.getItem(VOICE_KEY + L) } catch (_) { return null }
}

// The reader's own choice; then the system default; then the region the
// reader's browser asks for (en-GB user, en-GB voice); then offline over
// online, because it starts at once and works on a plane.
function pickVoice(L) {
  const list = voicesFor(L);
  if (!list.length) return null;
  const uri = storedVoice(L);
  const mine = uri && list.find(v => v.voiceURI === uri);
  if (mine) return mine;
  const want = (navigator.languages || [navigator.language])
    .map(x => String(x).replace(/_/g, "-").toLowerCase()).find(x => x.includes("-") && norm(x) === L);
  const score = v => (v.default ? 8 : 0) +
    (want && String(v.lang).replace(/_/g, "-").toLowerCase() === want ? 4 : 0) +
    (v.localService ? 2 : 0);
  return list.reduce((b, v) => score(v) > score(b) ? v : b);
}

// ---------------------------------------------------------------- speech
let sess = 0, busy = false, poll = 0;
const live = new Set(); // Chrome collects an unreferenced utterance and its `end` never comes

function chunks(text, L) {
  let sents;
  try { sents = Array.from(new Intl.Segmenter(L, { granularity: "sentence" }).segment(text), s => s.segment) }
  catch (_) { sents = text.match(/[^.!?。！？]*(?:[.!?。！？]+\s*|$)/g) || [text] }
  const out = [];
  let cur = "";
  for (const s of sents) {
    if ((cur + s).length <= CHUNK) { cur += s; continue }
    if (cur) out.push(cur);
    let rest = s;
    while (rest.length > CHUNK) {
      const p = Math.max(rest.lastIndexOf(", ", CHUNK), rest.lastIndexOf("; ", CHUNK));
      let cut = p > CHUNK / 2 ? p + 1 : rest.lastIndexOf(" ", CHUNK);
      if (cut < CHUNK / 2) cut = CHUNK; // no spaces: CJK, or one enormous word
      out.push(rest.slice(0, cut));
      rest = rest.slice(cut);
    }
    cur = rest;
  }
  if (cur) out.push(cur);
  return out.map(s => s.trim()).filter(Boolean);
}

function stop() {
  sess++;
  clearInterval(poll);
  live.clear();
  try { S() && S().cancel() } catch (_) { }
  setBusy(false);
}

function say(text, L, voice) {
  const syn = S(), U = window.SpeechSynthesisUtterance;
  if (!syn || !U || !text) return;
  stop();
  const tok = sess, parts = chunks(text, L);
  let i = 0, cur = null, idle = 0;
  const next = () => {
    if (tok !== sess) return;
    if (i >= parts.length) { done(); return }
    const u = new U(parts[i++]);
    let ended = false;
    const end = () => {
      if (ended) return;
      ended = true;
      live.delete(u);
      if (tok === sess && u === cur) next();
    };
    cur = u;
    u.lang = voice ? voice.lang : L;
    if (voice) u.voice = voice;
    u.onend = end;
    u.onerror = e => {
      const er = e && e.error;
      ended = true;
      live.delete(u);
      if (tok !== sess || u !== cur) return;
      // Our own stop() moves `sess` first, so an interruption still in this
      // session came from outside - the OS, another app's audio, the app
      // leaving the screen. It ends the reading; the watchdog must not take
      // the silence for an end and carry on at the next sentence.
      if (er === "interrupted" || er === "canceled") { stop(); done(); return }
      fail(er, L);
    };
    live.add(u);
    try { syn.resume() } catch (_) { } // Chrome can sit paused with nothing said
    syn.speak(u);
    idle = 0;
  };
  // A lost `end` must not leave Stop on screen forever: an engine that has
  // gone quiet with nothing queued has finished, whatever it reported.
  poll = setInterval(() => {
    if (tok !== sess) return;
    if (syn.speaking || syn.pending) { idle = 0; return }
    if (++idle >= 3 && cur) cur.onend(); // guarded: a late real `end` is a no-op
  }, 700);
  setBusy(true);
  next();
}

function done() {
  clearInterval(poll);
  setBusy(false);
  if (!selLive) hide();
}

function fail(er, L) {
  clearInterval(poll);
  setBusy(false);
  if (er === "language-unavailable" || er === "voice-unavailable" || er === "synthesis-unavailable")
    openMenu(noVoice(L));
  else console.warn("wudict: read aloud failed:", er || "unknown error");
}

const noVoice = L => "No voice for " + langName(L) + " on this device";

// ------------------------------------------------------------- the stash
// What was selected when it settled. Speaking reads this, never the live
// selection: the tap on the icon may itself collapse the selection (Android
// does), and the text has to survive it.
let stash = null, selLive = false, ctx = null;

function setStash(st) {
  const L = ctx && ctx.langs ? ctx.langs(st.dict) : { a: "", h: "" };
  const a = norm(L.a), h = norm(L.h);
  st.text = st.text.replace(/\s+/g, " ").trim().slice(0, MAX);
  st.sc = scriptOf(st.text);
  st.lang = decide(st.sc, a, h);
  st.alts = [...new Set([a, h])].filter(l => l && l !== st.lang && (!st.sc || fits(l, st.sc)));
  stash = st;
  selLive = true;
  startVoices();
  show();
}

// ------------------------------------------------------------------ UI
const SVG = 'viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"';
const ICON_SPEAK = '<svg ' + SVG + '><path d="M2.5 6h2.5l3.5-3v10l-3.5-3H2.5z"/><path d="M11 5.8a3 3 0 0 1 0 4.4"/><path d="M12.9 3.9a5.8 5.8 0 0 1 0 8.2"/></svg>';
const ICON_STOP = '<svg ' + SVG + '><rect x="4" y="4" width="8" height="8" rx="1.3" fill="currentColor"/></svg>';
const ICON_MORE = '<svg ' + SVG + '><path d="M4.5 6.5l3.5 3.5 3.5-3.5"/></svg>';
const CSS =
  ".wd-speak{position:fixed;z-index:150;display:flex;background:var(--bg-card,#fff);color:var(--fg,#222);" +
  "border:1px solid var(--line,#ddd);border-radius:999px;box-shadow:var(--shadow,0 2px 8px rgba(0,0,0,.2));overflow:hidden;" +
  "user-select:none;-webkit-user-select:none;-webkit-touch-callout:none;touch-action:manipulation}" +
  ".wd-speak[hidden],.wd-speak button[hidden],.wd-sp-menu[hidden]{display:none}" +
  ".wd-speak button{all:unset;display:flex;align-items:center;justify-content:center;cursor:pointer;" +
  "color:inherit;-webkit-tap-highlight-color:transparent;width:30px;height:30px}" +
  ".wd-speak .wd-sp-more{width:18px;border-left:1px solid var(--line-soft,#eee)}" +
  ".wd-speak svg{width:16px;height:16px}.wd-speak .wd-sp-more svg{width:12px;height:12px}" +
  ".wd-speak.touch button{width:48px;height:48px}.wd-speak.touch .wd-sp-more{width:30px}" +
  ".wd-speak.touch svg{width:22px;height:22px}.wd-speak.touch .wd-sp-more svg{width:14px;height:14px}" +
  ".wd-speak.busy .wd-sp-main{color:var(--accent,#e08600)}" +
  ".wd-speak button:hover{background:var(--accent-soft,#fff3e0)}" +
  ".wd-speak button:focus-visible,.wd-sp-menu button:focus-visible{outline:2px solid var(--focus,#7aa7d1);outline-offset:-2px}" +
  ".wd-sp-menu{position:fixed;z-index:151;min-width:12em;max-width:min(22em,calc(100vw - 16px));max-height:50vh;" +
  "overflow:auto;background:var(--bg-card,#fff);color:var(--fg,#222);border:1px solid var(--line,#ddd);" +
  "border-radius:10px;box-shadow:var(--shadow,0 2px 8px rgba(0,0,0,.2));padding:4px 0;" +
  "font:14px/1.35 var(--font,system-ui,sans-serif);user-select:none;-webkit-user-select:none}" +
  ".wd-sp-menu button{all:unset;box-sizing:border-box;display:flex;gap:.8em;align-items:baseline;width:100%;" +
  "padding:9px 12px;cursor:pointer}" +
  ".wd-sp-menu button:hover{background:var(--accent-soft,#fff3e0)}" +
  ".wd-sp-menu button[aria-checked=true]{color:var(--accent,#e08600)}" +
  ".wd-sp-menu small{margin-left:auto;color:var(--fg-faint,#999);font-size:12px}" +
  ".wd-sp-menu .note{padding:9px 12px;color:var(--fg-soft,#666)}" +
  ".wd-sp-menu hr{border:0;border-top:1px solid var(--line-soft,#eee);margin:4px 0}";

let ui = null, mainBtn = null, moreBtn = null, menu = null, menuNote = "";
let touch = false, raf = 0;

function build() {
  if (ui) return;
  const st = document.createElement("style");
  st.textContent = CSS;
  document.head.appendChild(st);
  ui = document.createElement("div");
  ui.className = "wd-speak";
  ui.hidden = true;
  ui.innerHTML = '<button type="button" class="wd-sp-main"></button>' +
    '<button type="button" class="wd-sp-more" aria-haspopup="menu" aria-expanded="false" ' +
    'aria-label="Voice" title="Voice">' + ICON_MORE + "</button>";
  mainBtn = ui.firstChild;
  moreBtn = ui.lastChild;
  menu = document.createElement("div");
  menu.className = "wd-sp-menu";
  menu.setAttribute("role", "menu");
  menu.hidden = true;
  // Pressing the icon must not take the selection or the focus away from the
  // article; the action happens on click, which is the gesture Safari and
  // Chrome require before they will speak.
  for (const el of [ui, menu]) el.addEventListener("pointerdown", e => e.preventDefault());
  mainBtn.addEventListener("click", () => {
    closeMenu();
    if (busy) { stop(); if (!selLive) hide(); return }
    if (stash) go(stash.lang, null);
  });
  moreBtn.addEventListener("click", e => {
    if (!menu.hidden) { closeMenu(); return }
    openMenu("", e.detail === 0);
  });
  document.body.append(ui, menu);
  setBusy(busy);
}

function setBusy(v) {
  busy = v;
  if (!mainBtn) return;
  ui.classList.toggle("busy", v);
  mainBtn.innerHTML = v ? ICON_STOP : ICON_SPEAK;
  const label = v ? "Stop reading" : "Read aloud";
  mainBtn.setAttribute("aria-label", label);
  mainBtn.title = label;
}

function go(L, voice) {
  const v = voice || pickVoice(L);
  if (!v && voicesDone) { openMenu(noVoice(L)); return }
  say(stash.text, L, v);
}

// The ▾ is worth its room only when it has a choice to offer.
function syncMore() {
  if (!moreBtn || !stash) return;
  moreBtn.hidden = !(stash.alts.length || voicesFor(stash.lang).length > 1 ||
    (voicesDone && !voicesFor(stash.lang).length) || settings());
}

const settings = () => window.__wdSpeech && typeof window.__wdSpeech.settings === "function";

function show() {
  build();
  touch = stash.ptr === "touch";
  ui.classList.toggle("touch", touch);
  syncMore();
  ui.hidden = false;
  place();
}

function hide() {
  if (!ui) return;
  closeMenu();
  if (busy) return; // Stop stays until the reading ends
  ui.hidden = true;
  cancelAnimationFrame(raf);
}

// Where the selection ends, in viewport pixels; null when it is gone.
function anchorRect() {
  if (!stash) return null;
  if (stash.kind === "frame") {
    const f = stash.frame, r = stash.rect;
    if (!f.isConnected || !r) return null;
    const b = f.getBoundingClientRect();
    return { left: b.left + r.x, top: b.top + r.y, right: b.left + r.x + r.w, bottom: b.top + r.y + r.h, height: r.h };
  }
  if (!stash.range) return null;
  const rs = stash.range.getClientRects();
  const b = rs.length ? rs[rs.length - 1] : stash.range.getBoundingClientRect();
  return b.width || b.height ? b : null;
}

// Touch: docked in the thumb zone, where neither the system's selection menu
// (above the selection) nor its handles (below it) can ever be. Mouse: just
// after the end of the selection, where the pointer already is.
function place() {
  if (!ui || ui.hidden) return;
  if (touch) {
    ui.style.cssText = "right:16px;bottom:calc(24px + var(--wd-inset-bottom,0px))";
    return;
  }
  const r = selLive ? anchorRect() : null;
  if (!r) { if (!busy) ui.hidden = true; return } // busy with no anchor: stay where it was
  const w = ui.offsetWidth, h = ui.offsetHeight, top = ctx && ctx.top ? ctx.top() : 0;
  let x = r.right + 6, y = r.top + (r.height - h) / 2;
  if (x + w > innerWidth - 8) { x = Math.max(8, r.right - w); y = r.bottom + 6 }
  const off = y < top || y + h > innerHeight;
  ui.style.cssText = "left:" + Math.round(x) + "px;top:" + Math.round(y) + "px" + (off ? ";visibility:hidden" : "");
  if (!menu.hidden) placeMenu();
}

function track() {
  if (touch || !ui || ui.hidden) return;
  cancelAnimationFrame(raf);
  raf = requestAnimationFrame(place);
}

function openMenu(note, focusFirst) {
  if (!stash) return;
  build();
  menuNote = note || "";
  menu.textContent = "";
  const add = (label, tag, fn, checked) => {
    const b = document.createElement("button");
    b.type = "button";
    b.setAttribute("role", checked == null ? "menuitem" : "menuitemradio");
    if (checked != null) b.setAttribute("aria-checked", checked ? "true" : "false");
    b.append((checked ? "✓ " : "") + label);
    if (tag) { const s = document.createElement("small"); s.textContent = tag; b.append(s) }
    b.addEventListener("click", () => { closeMenu(); fn() });
    menu.append(b);
  };
  const rule = () => { if (menu.childNodes.length) menu.append(document.createElement("hr")) };
  if (menuNote) {
    const d = document.createElement("div");
    d.className = "note";
    d.textContent = menuNote;
    menu.append(d);
  }
  if (stash.alts.length) {
    rule();
    for (const l of stash.alts) add("Read as " + langName(l), "", () => go(l, null));
  }
  const L = stash.lang, vs = voicesFor(L);
  if (vs.length) {
    rule();
    const cur = pickVoice(L);
    for (const v of vs.slice().sort((a, b) => String(a.label || a.name).localeCompare(String(b.label || b.name))))
      add(v.label || v.name, v.lang, () => {
        try { localStorage.setItem(VOICE_KEY + L, v.voiceURI) } catch (_) { }
        say(stash.text, L, v); // choosing IS the preview
      }, v === cur);
  }
  if (settings()) { rule(); add("Speech settings…", "", () => window.__wdSpeech.settings()) }
  if (!menu.childNodes.length) return;
  menu.hidden = false;
  moreBtn.setAttribute("aria-expanded", "true");
  if (ui.hidden) { ui.hidden = false; place() }
  placeMenu();
  if (focusFirst) { const b = menu.querySelector("button"); if (b) b.focus() }
}

function placeMenu() {
  const b = ui.getBoundingClientRect(), mw = menu.offsetWidth, mh = menu.offsetHeight;
  const x = Math.max(8, Math.min(b.right - mw, innerWidth - mw - 8));
  const below = b.bottom + 6 + mh <= innerHeight - 8;
  const y = touch || !below ? Math.max(8, b.top - 6 - mh) : b.bottom + 6;
  menu.style.left = Math.round(x) + "px";
  menu.style.top = Math.round(y) + "px";
}

function closeMenu() {
  if (!menu || menu.hidden) return;
  menu.hidden = true;
  menuNote = "";
  moreBtn.setAttribute("aria-expanded", "false");
}

// ------------------------------------------------------------ selections
// Shadow-DOM articles. The standard read is getComposedRanges; Chrome before
// it had only ShadowRoot.getSelection, and older Firefox exposed shadow nodes
// through the document's own ranges.
let root = null, ptr = "mouse", btn = false, settleT = 0;

function readShadow() {
  const host = root && root.host;
  if (!host || !host.isConnected) return null;
  const doc = document.getSelection();
  // Chrome's own shadow selection renders the text as laid out (line breaks,
  // no hidden nodes); a bare range's toString is the fallback elsewhere.
  const sel = root.getSelection ? root.getSelection() : null;
  let range = null;
  if (doc && doc.getComposedRanges) {
    let sr = null;
    try { sr = doc.getComposedRanges({ shadowRoots: [root] })[0] }
    catch (_) { try { sr = doc.getComposedRanges(root)[0] } catch (_) { } }
    if (sr && !sr.collapsed) {
      try {
        range = document.createRange();
        range.setStart(sr.startContainer, sr.startOffset);
        range.setEnd(sr.endContainer, sr.endOffset);
      } catch (_) { range = null }
    }
  }
  if (!range) {
    const s = sel || doc;
    if (!s || !s.rangeCount) return null;
    range = s.getRangeAt(0).cloneRange();
  }
  if (range.collapsed || range.commonAncestorContainer.getRootNode() !== root) return null;
  const text = ((sel && String(sel)) || range.toString()).trim();
  return text ? { kind: "shadow", text, range, dict: host.dataset.dict || "", ptr } : null;
}

function settle() {
  if (btn) return; // the drag is still going; pointerup settles it
  const st = readShadow();
  if (st) { setStash(st); return }
  if (stash && stash.kind === "shadow") { selLive = false; hide() }
}

function schedule(ms) { clearTimeout(settleT); settleT = setTimeout(settle, ms) }

function onDown(e) {
  if (ui && (ui.contains(e.target) || menu.contains(e.target))) return;
  closeMenu();
  // A frame keeps its selection when the host is clicked, and reports nothing.
  if (stash && stash.kind === "frame") { selLive = false; hide() }
  ptr = e.pointerType || "mouse";
  if (ptr === "mouse" && e.button === 0) btn = true;
  const host = e.composedPath().find(el => el.classList && el.classList.contains("article"));
  root = (host && host.shadowRoot) || null;
}
function onUp(e) {
  if (!btn || e.pointerType !== "mouse") return;
  btn = false;
  schedule(120);
}
// Android's selection handles deliver nothing to the page while they are
// dragged; a selection that has stopped changing is the only signal there is.
function onSel() { schedule(ptr === "touch" ? 350 : 200) }
function onKey(e) {
  if (e.key !== "Escape") return;
  if (menu && !menu.hidden) { closeMenu(); return }
  if (busy) { stop(); if (!selLive) hide() }
}
function onScroll() { track() }

let on = false;
function enable(v) {
  if (v === on) return;
  on = v;
  const f = (v ? window.addEventListener : window.removeEventListener).bind(window);
  const d = (v ? document.addEventListener : document.removeEventListener).bind(document);
  d("selectionchange", onSel);
  d("pointerdown", onDown, true);
  d("pointerup", onUp, true);
  d("keydown", onKey, true);
  f("scroll", onScroll, { capture: true, passive: true });
  f("resize", onScroll, { passive: true });
  if (!v) { stop(); selLive = false; stash = null; hide() }
}

window.wuSpeak = {
  // ctx: { langs(dictID) -> {a, h}, top() -> px the bar covers };
  // seed: the article root and pointer the selection that woke us came from.
  on(v, c, seed) {
    if (c) ctx = c;
    enable(!!v);
    if (v && seed) { root = seed.root || root; ptr = seed.ptr || ptr; schedule(0) }
  },
  // A selection inside a sandboxed (iframe) article, reported by frame.js.
  frame(f, text, r, p) {
    if (!on || !f) return;
    text = String(text || "");
    if (!text.trim()) {
      if (stash && stash.kind === "frame" && stash.frame === f) { selLive = false; hide() }
      return;
    }
    const rect = r && isFinite(r.x) && isFinite(r.y) && isFinite(r.w) && isFinite(r.h) ?
      { x: +r.x, y: +r.y, w: +r.w, h: +r.h } : null;
    setStash({ kind: "frame", frame: f, rect, text, dict: f.dataset.dict || "", ptr: p === "touch" ? "touch" : "mouse" });
  }
};
})();
