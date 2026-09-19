// Search history and live headword suggestions for the search field.
// The page supplies only the ordinary search action; this file owns storage,
// rendering, input events and the history-length control.
(function () {
  "use strict";
  const $ = id => document.getElementById(id);
  const MAX = 100;
  const key = s => s.normalize("NFC").toLocaleLowerCase();
  const limit = () => {
    const raw = localStorage.getItem("wudict_history_length");
    if (raw === null) return 20;
    const n = Number(raw);
    return Number.isInteger(n) && n >= 0 && n <= MAX ? n : 20;
  };
  let saved = [];
  try {
    const rows = JSON.parse(localStorage.getItem("wudict_search_history") || "[]");
    if (Array.isArray(rows)) {
      const seen = new Set();
      for (const word of rows) {
        if (typeof word !== "string" || !word.trim()) continue;
        const id = key(word);
        if (seen.has(id)) continue;
        seen.add(id);
        saved.push(word);
        if (saved.length === MAX) break;
      }
    }
  } catch (_) {}
  saved = saved.slice(0, limit());
  let draft = false, open = false, heads = [], picking = false, search = () => {}, suggest = () => {};
  let suggestTimer = null, suggestAC = null, suggestSeq = 0;
  function stopSuggest() {
    clearTimeout(suggestTimer);
    if (suggestAC) suggestAC.abort();
    suggestAC = null;
    suggestSeq++;
  }

  function persist() {
    try { localStorage.setItem("wudict_search_history", JSON.stringify(saved)); } catch (_) {}
    $("clearHistory").disabled = !saved.length;
  }
  function record(term) {
    draft = false;
    const word = String(term || "").trim(), max = limit();
    if (!word || !max) return;
    saved = [word, ...saved.filter(x => key(x) !== key(word))].slice(0, max);
    persist();
  }
  function hide() { stopSuggest(); open = false; $("searchHistory").hidden = true; }
  function size() {
    const box = $("searchHistory");
    if (!matchMedia("(max-width:600px)").matches) { box.style.right = ""; return; }
    const pill = document.querySelector("#frm>.pill");
    const extra = pill.getBoundingClientRect().right - $("qbox").getBoundingClientRect().right;
    box.style.right = -Math.max(0, extra - 1) + "px";
  }
  function render(filter = "") {
    if (!open) return;
    const box = $("searchHistory");
    size();
    box.replaceChildren();
    const needle = key(filter.trim()), seen = new Set();
    const maxRows = Math.max(20, limit());
    const words = needle ? [...heads, ...saved.slice(0, limit())] : saved.slice(0, limit());
    for (const word of words) {
      const id = key(word);
      if ((needle && !id.includes(needle)) || seen.has(id)) continue;
      seen.add(id);
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = word;
      button.addEventListener("click", () => { picking = false; hide(); search(word); });
      box.appendChild(button);
      if (box.childElementCount >= maxRows) break;
    }
    box.hidden = !box.childElementCount;
  }
  function show(filter = "") { open = true; render(filter); }
  function onInput(value) {
    draft = true;
    refreshSuggestions(value);
  }
  function refreshSuggestions(value = $("q").value) {
    stopSuggest();
    heads = [];
    show(value);
    const q = value.trim();
    if (q.length < 2 || $("mode").value === "fts") return;
    const seq = suggestSeq;
    suggestTimer = setTimeout(() => {
      suggestAC = new AbortController();
      Promise.resolve(suggest(q, (results, mode) => {
        if (seq === suggestSeq) addResults(q, results, mode);
      }, suggestAC.signal)).catch(err => {
        if (err?.name !== "AbortError") console.warn("Suggestions:", err);
      });
    }, 300);
  }
  function addResults(q, results, mode) {
    if (mode === "fts" || !open || $("q").value.trim() !== q || !results?.length) return;
    const seen = new Set(heads.map(key));
    for (const result of results) {
      const word = String(result.Headword || "").trim(), id = key(word);
      if (!word || seen.has(id)) continue;
      seen.add(id);
      heads.push(word);
      if (heads.length >= MAX) break;
    }
    render(q);
  }

  $("historyLength").value = limit();
  $("clearHistory").disabled = !saved.length;
  $("clearHistory").addEventListener("click", () => {
    saved = [];
    draft = false;
    persist();
    render($("q").value);
  });
  $("historyLength").addEventListener("change", e => {
    const n = Math.max(0, Math.min(MAX, Math.trunc(Number(e.target.value) || 0)));
    e.target.value = n;
    try { localStorage.setItem("wudict_history_length", String(n)); } catch (_) {}
    saved = saved.slice(0, n);
    persist();
    if (!n) hide();
  });
  $("q").addEventListener("focus", () => show());
  $("q").addEventListener("pointerdown", () => show());
  $("searchHistory").addEventListener("pointerdown", () => { draft = false; picking = true; });
  $("searchHistory").addEventListener("pointercancel", () => { picking = false; });
  document.addEventListener("pointerdown", e => {
    if (!$("qbox").contains(e.target)) hide();
  });
  $("q").addEventListener("blur", () => {
    if (draft && $("q").value.trim().length >= 2) record($("q").value);
  });
  $("q").addEventListener("keydown", e => {
    if (e.key === "Escape") { hide(); return; }
    if (e.key === "ArrowDown" && !$("searchHistory").hidden) {
      e.preventDefault();
      $("searchHistory").firstElementChild?.focus();
    }
  });
  $("searchHistory").addEventListener("keydown", e => {
    if (e.key === "Escape") { hide(); $("q").focus(); }
  });
  const sizer = new ResizeObserver(() => { if (open) size(); });
  sizer.observe(document.querySelector("#frm>.pill"));
  sizer.observe($("qbox"));

  window.wuSearchHistory = { setSearch(fn) { search = fn; }, setSuggest(fn) { suggest = fn; }, record, hide, onInput, refreshSuggestions, isPicking() { return picking; } };
})();
