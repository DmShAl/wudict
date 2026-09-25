// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Saved appearances: the Presets row on the panel, the menu it opens, the
// window that names a new one, and the question asked before a switch throws
// away changes nobody has saved.
//
// The state is the SERVER's - it reads the settings, the four stylesheets and
// the enabled layers for itself. What this file adds is the shell's half: the
// window colour, the wallpaper, the margins and the bars live in Android's
// SharedPreferences, which the server cannot reach, so the page reports them
// on the way in and pushes them back on the way out. That is also why the row
// works in a plain browser: with no shell there is no half to send, and the
// server stores a look without a backdrop.

const LOOK_SAVE_LABEL = "Save Current…";

let LOOKS = null; // the payload of GET /api/looks
// The menu's labels, MUTATED in place rather than replaced: screenChoice
// closes over the array it was given, so a new array would leave the menu
// reading the old list for ever.
const LOOK_LABELS = [];
let looksChoice = null;
// The look the reader asked for, held while a question is on screen: the
// buttons in that window all end in "and then go there".
let looksPending = null;

function looksEl(id) { return document.getElementById(id); }

async function looksFetch(path, method, body) {
  const r = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) throw new Error((await r.text()).trim() || "HTTP " + r.status);
  return r.json();
}

/* What this page knows about the screen, in the shape every save and every
   apply sends. The two text settings go with the shell's half for the same
   reason it does: the file they are written to lags a tap by 400ms, and the
   comparison that decides whether to ask about unsaved changes must see the
   size the reader just chose, not the one before it. */
function looksNow() {
  return { shell: looksShellNow(), fontSize: articleFS, fontWeight: articleFW };
}

/* The shell's half, as the reader's window has it now. BOTH themes are asked
   for, because a look carries a day value and a night one and the shell keeps
   them in separate slots; the answer that comes back for the theme on screen
   also carries the margins and the bars, which are not per theme. */
function looksShellNow() {
  if (!window.wudictNativeShell) return null;
  const day = appearanceRequest({ action: "get", night: false }) || {};
  const night = appearanceRequest({ action: "get", night: true }) || {};
  const half = (s) => ({
    colorEnabled: !!s.colorEnabled,
    color: s.color || "",
    image: s.image || "",
  });
  return {
    light: half(day),
    dark: half(night),
    edgeMode: Number.isInteger(day.edgeMode) ? day.edgeMode : undefined,
    edgeColor: day.edgeColor || "",
    bars: Number.isInteger(day.bars) ? day.bars : undefined,
  };
}

/* The shell's half back, written where it belongs. Written through the bridge
   directly rather than through appearanceSet, because that one renders what it
   wrote - and half of what is written here is the OTHER theme's, which must not
   be drawn into the sheet as if it were this theme's. appearanceRead puts the
   sheet right once, at the end.

   An empty colour is not written: the shell parses what it is given and an
   empty string is a refusal, while a look with no colour simply leaves that
   theme's colour where it is. Nothing reads it anyway - the checkbox beside it
   is what says whether the colour is in use. */
function looksPushShell(half) {
  if (!half || !window.wudictNativeShell) return;
  // Each half goes to the slot it belongs to, NOT to whichever theme happens
  // to be on screen: a look applied at night was writing its LIGHT half into
  // the night slot, which is how Sepia came to have the colour checkbox ticked
  // at night - the one place its look says nothing.
  for (const [night, side] of [[false, half.light], [true, half.dark]]) {
    if (!side) continue;
    appearanceRequest({ action: "set", night, field: "colorEnabled", value: !!side.colorEnabled });
    if (side.color) appearanceRequest({ action: "set", night, field: "color", value: side.color });
    appearanceRequest({ action: "set", night, field: "image", value: side.image || "" });
  }
  if (half.edgeMode !== undefined) appearanceRequest({ action: "set", field: "edgeMode", value: half.edgeMode });
  if (half.edgeColor) appearanceRequest({ action: "set", field: "edgeColor", value: half.edgeColor });
  if (half.bars !== undefined) appearanceRequest({ action: "set", field: "bars", value: half.bars });
  appearanceRead();
}

// ── the row ──────────────────────────────────────────────────────────────

/* The row, drawn from two facts: which look is in force, and whether the
   screen still matches it. The second one is what the words depend on.
   - It matches: the answer IS that look, whoever it belongs to.
   - It does not, and the look is the reader's own: the name still stands,
     because the reader can fold the change straight back into it ("Update").
   - It does not, and the look is a built-in - or nothing is in force at all:
     the answer is not that look any more, and "Custom" says so. Nothing is
     ticked then either, because a tick on Clean beside the word Custom is two
     answers to one question.
   The two buttons beside the words are the actions they leave open, and both
   are disabled rather than hidden: the row keeps its shape, and a control that
   comes and goes has to be found again. */
function looksRowRender() {
  if (!LOOKS) return;
  const cur = LOOKS.looks.find((l) => l.id === LOOKS.current) || null;
  const custom = !cur || (LOOKS.currentDrifted && cur.builtin);
  looksChoice.set(custom ? -1 : LOOKS.looks.indexOf(cur));
  if (custom) looksEl("looksChoice").textContent = "Custom";

  const save = looksEl("looksSaveAsBtn"), upd = looksEl("looksUpdateBtn");
  // Update exists only where it can work: a look the reader owns.
  upd.hidden = !cur || cur.builtin;
  save.disabled = !LOOKS.currentDrifted;
  upd.disabled = !LOOKS.currentDrifted;
}

function looksRender() {
  if (!LOOKS) return;
  LOOK_LABELS.length = 0;
  if (!LOOKS.writable) {
    LOOK_LABELS.push("No configuration folder — a look has nowhere to live");
    looksChoice.set(0);
    return;
  }
  for (const l of LOOKS.looks) LOOK_LABELS.push(l.name);
  LOOK_LABELS.push(LOOK_SAVE_LABEL);
  looksRowRender();
}

async function looksLoad() {
  try {
    LOOKS = await looksFetch("/api/looks", "GET");
  } catch (e) {
    console.warn("could not load looks:", e);
    return;
  }
  looksRender();
  await looksCheckDrift();
}

function looksLabelFor(id) {
  const l = LOOKS && LOOKS.looks.find((x) => x.id === id);
  return l ? l.name : id;
}

// ── applying ─────────────────────────────────────────────────────────────

/* The first pass writes nothing: it only asks whether the screen has drifted
   from the look in force, which is the reader's cue to save it first. */
async function looksApply(id) {
  let answer;
  try {
    answer = await looksFetch("/api/looks/apply", "POST", Object.assign({ id }, looksNow()));
  } catch (e) {
    setStatus("⚠ could not apply the preset: " + e.message);
    return;
  }
  if (answer.needsConfirm) {
    looksPending = id;
    looksAsk(answer.current || {});
    return;
  }
  await looksApplied(answer);
}

async function looksApplied(answer) {
  LOOKS = answer.payload;
  // The PAGE's own halves go back FIRST: the backdrop over the bridge, and the
  // size and the weight onto the element - the server's copy of those is a
  // file, and neither the stepper nor the article's custom property reads one.
  // The drift check runs LAST, on purpose: it compares what is on screen with
  // what the look says, and run before these it compared the OLD screen and
  // left the buttons enabled for a state that already matched.
  looksPushShell(answer.shell);
  if (answer.fontSize !== undefined) applyFS(answer.fontSize, false);
  if (answer.fontWeight !== undefined) applyFW(answer.fontWeight, false);
  await looksReloadSheets();
  await looksCheckDrift();
  looksRender();
  const name = looksLabelFor(answer.applied);
  setStatus("Presets: “" + name + "” applied");
  setTimeout(() => setStatus(""), 2600);
}

/* The page's own copy of the stylesheets and the layer switches, after the
   server has rewritten them. Without this the apply would land on disk and
   nowhere else: the app's own <link>s are stamped by the server at render
   time, and the editor would go on showing the sheets that were just
   replaced. */
async function looksReloadSheets() {
  try {
    const j = await looksFetch("/api/style", "GET");
    const night = themeIsDark();
    stylerText = stylePairFrom(j, night);
    stylerSaved = { ...stylerText };
    stylerOther = { text: stylePairFrom(j, !night), saved: stylePairFrom(j, !night) };
    stylerLoaded = true;
    stylerPreviewAll(stylerText);
    // stylerShowTab folds the textarea back into stylerText first; the box
    // holds the sheet that was just replaced, and folding it back would put
    // the replaced text straight into the fresh pair.
    if ($("styler").classList.contains("show")) {
      stylerBox = null;
      if (stylerSubject === "css") stylerShowTab(stylerTab);
    }
  } catch (e) {
    console.warn("could not reload the stylesheets:", e);
  }
  if (await presetsLoad()) {
    if (stylerSubject === "presets") presetsPaneRender();
  } else {
    // A failed layer read leaves the old links in place; saying so is better
    // than a page that shows half of two looks.
    setStatus("⚠ the layers could not be reloaded — reload the page");
  }
}

// ── the question ─────────────────────────────────────────────────────────

function looksAsk(current) {
  const target = looksLabelFor(looksPending);
  const from = current.name || "";
  looksEl("lookAskBody").textContent = from
    ? "You have changed “" + from + "” since it was applied. Switching to “" + target +
      "” will replace those changes — save them first?"
    : "The screen has been set by hand and no look is in force. Switching to “" + target +
      "” will replace those changes — save them first?";
  const upd = looksEl("lookAskUpdate");
  // Only a look the reader owns can be updated; a built-in is the app's, and
  // a built-in called "Clean" that means something else would be a lie.
  if (current.id && !current.builtin) {
    upd.hidden = false;
    upd.textContent = "Update “" + from + "”";
  } else {
    upd.hidden = true;
  }
  looksEl("lookAskError").textContent = "";
  looksEl("lookAskDialog").showModal();
}

async function looksAskUpdate() {
  await looksUpdateCurrent();
  // Saved, so the same apply passes its check and goes through.
  if (looksPending) await looksApply(looksPending);
}

function looksCurrentID() {
  return LOOKS && LOOKS.current ? LOOKS.current : "";
}

// ── saving ───────────────────────────────────────────────────────────────

function looksOpenSave(name) {
  looksEl("lookName").value = name || "";
  looksEl("lookSaveError").textContent = "";
  looksEl("lookSaveDialog").showModal();
  looksEl("lookName").focus();
}

async function looksSaveSubmit() {
  const err = looksEl("lookSaveError");
  const name = looksEl("lookName").value.trim();
  if (!name) {
    err.textContent = "Enter a name for it.";
    return;
  }
  err.textContent = "";
  const go = looksEl("lookSaveGo");
  go.disabled = true;
  try {
    LOOKS = await looksFetch("/api/looks", "POST", Object.assign({ name }, looksNow()));
    looksRender();
  } catch (e) {
    err.textContent = e.message;
    return;
  } finally {
    go.disabled = false;
  }
  looksEl("lookSaveDialog").close();
  if (looksPending) {
    // "Save them first" was the answer to a question about a switch that had
    // already been asked for: go there now.
    await looksApply(looksPending);
  }
}

/* Is the screen still the look in force? The dry pass answers that and writes
   nothing - it is the same comparison the switch question runs, asked on its
   own. It is what the button's presence hangs on, and the button is the one
   thing the drop-down cannot say: which of the two answers applies to what is
   on screen. */
async function looksCheckDrift() {
  if (!LOOKS || !LOOKS.writable) return;
  let answer;
  try {
    answer = await looksFetch("/api/looks/apply", "POST",
      Object.assign({ dry: true }, looksNow()));
  } catch (e) {
    // A check that cannot run leaves the row inert rather than lying: the words
    // stay as the last answer left them, and nothing is offered.
    LOOKS.currentDrifted = false;
    looksRowRender();
    return;
  }
  LOOKS.currentDrifted = !!answer.drifted;
  looksRowRender();
}

/* The action the ask dialog's Update button performs, on its own: the panel's
   button calls it without a question, because the reader is looking at the
   answer already. */
async function looksUpdateCurrent() {
  const err = looksEl("lookAskError");
  err.textContent = "";
  const upd = looksEl("lookAskUpdate");
  upd.disabled = true;
  try {
    LOOKS = await looksFetch("/api/looks", "PUT",
      Object.assign({ id: looksCurrentID() }, looksNow()));
    looksRender();
  } catch (e) {
    err.textContent = e.message;
    return;
  } finally {
    upd.disabled = false;
  }
  looksEl("lookAskDialog").close();
}

// ── wiring ───────────────────────────────────────────────────────────────

looksChoice = screenChoice(looksEl("looksChoice"), looksEl("looksMenu"), LOOK_LABELS, (i) => {
  if (!LOOKS || !LOOKS.writable) return;
  if (i === LOOK_LABELS.length - 1) {
    // The last row is the group window's "New Group": it makes a name rather
    // than choosing one, and it is NOT a switch, so nothing is applied after.
    looksPending = null;
    looksOpenSave("");
    return;
  }
  const look = LOOKS.looks[i];
  if (look) looksApply(look.id);
});

looksEl("lookSaveForm").onsubmit = (e) => {
  e.preventDefault();
  looksSaveSubmit();
};
looksEl("lookCancel").onclick = () => looksEl("lookSaveDialog").close();
looksEl("looksSaveAsBtn").onclick = () => {
  if (looksEl("looksSaveAsBtn").disabled) return;
  // A name is the only question: "Save as" makes a look, it never changes the
  // one in force.
  looksPending = null;
  looksOpenSave("");
};
looksEl("looksUpdateBtn").onclick = async () => {
  if (looksEl("looksUpdateBtn").disabled) return;
  await looksUpdateCurrent();
};
looksEl("lookAskUpdate").onclick = looksAskUpdate;
looksEl("lookAskSaveAs").onclick = () => {
  looksEl("lookAskDialog").close();
  looksOpenSave("");
};
looksEl("lookAskDiscard").onclick = async () => {
  looksEl("lookAskDialog").close();
  if (!looksPending) return;
  let answer;
  try {
    answer = await looksFetch("/api/looks/apply", "POST",
      Object.assign({ id: looksPending, confirm: true }, looksNow()));
  } catch (e) {
    setStatus("⚠ could not apply the preset: " + e.message);
    return;
  }
  await looksApplied(answer);
};
looksEl("lookAskCancel").onclick = () => {
  looksPending = null;
  looksEl("lookAskDialog").close();
};

looksLoad();
