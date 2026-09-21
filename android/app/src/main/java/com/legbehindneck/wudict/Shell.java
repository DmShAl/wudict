// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

// What every WebView in this app has in common: the one origin that belongs to
// us, how a URL is built for it, which settings the SPA needs, and where a
// navigation that is NOT ours goes instead.
//
// It lives here rather than in MainActivity because there are now two windows
// onto the same server - the app and the selection-lookup popup (D67) - and a
// second copy of this policy is a second thing to keep in step. Nothing here
// knows about either activity's layout or lifecycle.
package com.legbehindneck.wudict;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Environment;
import android.os.Message;
import android.provider.DocumentsContract;
import android.util.Log;
import android.webkit.CookieManager;
import android.webkit.JsPromptResult;
import android.webkit.JsResult;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebView;
import android.webkit.WebViewClient;

import java.io.File;
import java.io.UnsupportedEncodingException;
import java.net.URLEncoder;

final class Shell {

    private static final String TAG = "wudict";

    /**
     * The one origin that belongs to us; everything else is somebody's website.
     *
     * <p>A method rather than a constant since D101, because the port is an
     * install-level setting a device may override. The host never varies: it is
     * the address we connect to, not the one the server binds to.
     */
    static String origin(Context c) {
        return "http://" + ServerProcess.HOST + ":" + ServerProcess.port(c);
    }

    static String pageUrl(Context c) {
        String k = key(c);
        return origin(c) + "/?" + backgroundQuery(c) + (k.isEmpty() ? "" : "&" + k);
    }

    /**
     * The access key as a query fragment, or "" when this install does not use
     * one (ShellPrefs.REQUIRE_KEY).
     *
     * <p>Carried on the URL rather than installed with CookieManager, which
     * would be the obvious way and is the fragile one: setCookie's write is
     * not ordered against a loadUrl issued in the next statement, so the first
     * page of a cold start would race it. The server takes the key off the
     * URL, sets the cookie itself and redirects to the address without it -
     * one loopback round trip, no timing to reason about, and the key never
     * stays in the page's own location. Every subsequent request in that
     * WebView - articles, media, the search stream - rides the cookie.
     */
    private static String key(Context c) {
        String t = ShellPrefs.token(c);
        return t.isEmpty() ? "" : "k=" + t;
    }

    private Shell() {
    }

    /**
     * A search the page will run on load. `?q=…&mode=…&dict=…` is the SPA's own
     * deep-link shape (web/index.html, applyURL) - the shell adds no API and the
     * page learns nothing about Android (D54).
     *
     * <p>Every component is encoded here, so a caller's text - a selection from
     * another app, a URI from an intent - can carry anything at all.
     */
    static String searchUrl(Context c, String q, String mode, String dict) {
        StringBuilder b = new StringBuilder(origin(c)).append("/?q=").append(enc(q));
        if (mode != null && !mode.isEmpty()) b.append("&mode=").append(enc(mode));
        if (dict != null && !dict.isEmpty()) b.append("&dict=").append(enc(dict));
        String k = key(c);
        if (!k.isEmpty()) b.append("&").append(k);
        return b.append("&").append(backgroundQuery(c)).toString();
    }

    // Explicit empty value clears any cached override before the first paint.
    private static String backgroundQuery(Context c) {
        return "shell_bg=" + (ShellPrefs.sepia(c) ? enc(ShellPrefs.sepiaColorText(c)) : "")
                + "&shell_image=" + (WindowBackground.active(c) ? "1" : "0");
    }

    static void applyBackground(WebView web) {
        Context c = web.getContext();
        boolean image = WindowBackground.active(c);
        web.setBackgroundColor(image ? android.graphics.Color.TRANSPARENT : ShellPrefs.pageBg(c));
        String color = ShellPrefs.sepia(c) ? ShellPrefs.sepiaColorText(c) : "";
        // Only a validated six-digit colour is interpolated into JavaScript.
        web.evaluateJavascript("window.wudictShellBackground && window.wudictShellBackground('"
                + color + "'," + image + "," + org.json.JSONObject.quote(image
                ? ShellPrefs.of(c).getString("background_image", "") : "") + ")", null);
        // wudictNativeShell is how a page knows the shell answers its
        // wudict: prompts - the setup page's folder button speaks only when
        // it will be heard.
        web.evaluateJavascript("window.wudictNativeShell=1;"
                + "if(typeof appearanceRead==='function' && document.getElementById('styler')"
                + " && document.getElementById('styler').classList.contains('show')) appearanceRead();"
                + "(window.wudictSetDictionaryMode || function(found){"
                + "window.wudictFoundDictionaryMode=found;})("
                + ShellPrefs.foundDictionaries(c) + ");" + DICTIONARY_PICKER_JS, null);
    }

    // What the settings screen sends after emptying the browser's cache: the
    // window that shows the page belongs to another Activity, so it is asked to
    // load the page again rather than reached into.
    static final String EXTRA_RELOAD = "wudict.reload";

    /**
     * Empties the WebView's resource cache - the copies of the page's own files
     * (HTML, CSS, JS, the favicon) it keeps on the device.
     *
     * Only that. The dictionaries and the prepared library are the server's
     * files on disk, and localStorage (theme, wide mode, which group the picker
     * is on) is the reader's state, not a cache: neither is touched, which is
     * why this needs no confirmation and changes no setting.
     *
     * clearCache() is per-APPLICATION despite being an instance method - the
     * WebView documentation says so outright - so a throwaway WebView is the
     * whole requirement here.
     */
    static void clearWebCache(Context c) {
        WebView probe = new WebView(c);
        probe.clearCache(true);
        probe.destroy();
    }

    // Keep the real select as the source of truth, including streamed options,
    // groups, disabled entries and its existing change handler.
    static final String DICTIONARY_PICKER_JS = """
            (() => {
              for (const id of ['dict', 'mode', 'stylerPreset', 'groupSelect']) {
              const select = document.getElementById(id);
              if (!select || select.dataset.shellPicker) continue;
              select.dataset.shellPicker = '1';
              function open() {
                if (select.disabled) return;
                if (id === 'dict' && window.wudictNativeDictionaryPicker) {
                  window.wudictNativeDictionaryPicker();
                  return;
                }
                if (id === 'dict' && window.wudictFoundDictionaryMode && window.wudictFoundDictionaryPicker) {
                  window.wudictFoundDictionaryPicker();
                  return;
                }
                const options = Array.from(select.options), rows = [];
                let group = null;
                for (let index = 0; index < options.length; index++) {
                  const option = options[index];
                  if (id === 'stylerPreset' && !option.value) continue;
                  const parent = option.parentElement;
                  if (parent.tagName === 'OPTGROUP' && parent !== group) {
                    rows.push({label: parent.label, index: -1, disabled: true});
                  }
                  group = parent;
                  rows.push({label: option.textContent, index,
                    disabled: option.disabled || (parent.tagName === 'OPTGROUP' && parent.disabled)});
                }
                const answer = window.prompt('wudict:dictionary-picker',
                  JSON.stringify({rows, selected: select.selectedIndex, kind:id}));
                if (answer === null) return;
                const index = Number(answer);
                if (!Number.isInteger(index) || !options[index]) return;
                if (index !== select.selectedIndex) {
                  select.selectedIndex = index;
                  select.dispatchEvent(new Event('change', {bubbles:true}));
                }
              }
              let pointer = null, pointerClick = false, opening = false;
              function scheduleOpen() {
                if (opening) return;
                opening = true;
                // Let WebView finish the gesture before a native window takes focus.
                setTimeout(() => { try { open(); } finally { opening = false; } }, 0);
              }
              select.addEventListener('pointerdown', event => {
                if (event.button !== 0) return;
                event.preventDefault();
                pointer = {id:event.pointerId, x:event.clientX, y:event.clientY};
                pointerClick = true;
              });
              select.addEventListener('pointerup', event => {
                const start = pointer; pointer = null;
                if (!start || start.id !== event.pointerId) return;
                event.preventDefault();
                if (select.hasPointerCapture(event.pointerId)) select.releasePointerCapture(event.pointerId);
                if (Math.hypot(event.clientX-start.x, event.clientY-start.y) < 12) scheduleOpen();
              });
              select.addEventListener('pointercancel', () => {
                pointer = null;
              });
              // Accessibility activation can arrive as a click without a pointer.
              select.addEventListener('click', event => {
                event.preventDefault();
                if (pointerClick) { pointerClick = false; return; }
                if (event.detail === 0) scheduleOpen();
              });
              select.addEventListener('keydown', event => {
                if (['Enter',' ','ArrowDown','ArrowUp'].includes(event.key)) {
                  event.preventDefault();
                  pointerClick = false;
                  if (!event.repeat) scheduleOpen();
                }
              });
              }
            })();
            """;

    // URLEncoder writes ' ' as '+', which URLSearchParams reads back as ' ' -
    // the same pair the page already uses for its own links.
    private static String enc(String s) {
        try {
            return URLEncoder.encode(s, "UTF-8");
        } catch (UnsupportedEncodingException e) {
            return ""; // UTF-8 is guaranteed by the platform; this branch is unreachable
        }
    }

    /** The settings the SPA needs. Identical in every window, by construction. */
    static void configure(WebView web) {
        // The access key arrives as a cookie the server sets (see key()), so a
        // WebView that refuses cookies would authenticate once and then fail
        // every request the page makes. It is the platform default; stated
        // here because it is now load-bearing.
        CookieManager.getInstance().setAcceptCookie(true);
        web.getSettings().setJavaScriptEnabled(true);                 // the UI is one SPA
        web.getSettings().setDomStorageEnabled(true);                 // prefs live in localStorage
        web.getSettings().setMediaPlaybackRequiresUserGesture(false); // dictionary audio
        // D51 hands an article's external links to the page, which opens them
        // with window.open(). A WebView has no tabs, so that call is inert
        // unless multiple windows are supported and onCreateWindow answers it.
        web.getSettings().setSupportMultipleWindows(true);
    }

    /**
     * Reports whether it took the navigation off the WebView's hands. Our own
     * origin stays; a dictionary's link to a real website goes to whatever the
     * user browses with, because a WebView with no address bar, no tabs and no
     * back affordance is a trap to land a website in.
     */
    static boolean openExternal(Activity a, Uri uri) {
        if (uri == null) return false;
        String scheme = uri.getScheme();
        if (scheme == null) return false;
        scheme = scheme.toLowerCase();
        // The article machinery's own URLs: srcdoc frames, data: media, the
        // blob: URLs audio playback builds. None of those leave the app.
        if (scheme.equals("data") || scheme.equals("blob")
                || scheme.equals("about") || scheme.equals("javascript")) {
            return false;
        }
        String url = uri.toString();
        String origin = origin(a);
        if (url.equals(origin) || url.startsWith(origin + "/")) return false;
        // Shell-private URLs (wudict://…) are a channel from the page to the
        // Java side that costs no JavascriptInterface and no server API: this
        // method already inspects every navigation, so the branch is free.
        // Intake first, then the flavour's Storage - Intake owns the one
        // host that means the same thing in both flavours and delegates the
        // rest unchanged.
        if (Intake.handleShellUri(a, uri)) return true;
        try {
            a.startActivity(new Intent(Intent.ACTION_VIEW, uri)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
        } catch (ActivityNotFoundException | SecurityException e) {
            Log.w(TAG, "no app can open " + uri, e);
        }
        return true;
    }

    // The file picker <input type="file"> needs (D123: the styler's "Add…"
    // button). A WebView answers a file input with nothing whatsoever unless a
    // WebChromeClient implements onShowFileChooser - the page was never the
    // problem, the shell simply had no ear for it.
    //
    // Distinct from Storage.REQ_TREE: both flavours' onActivityResult and this
    // one are called for every result the activity receives.
    private static final int REQ_FILES = 0x5AF1;

    // The callback the page is blocked on. Static because the picker is another
    // activity and ours may be stopped - or, for the lookup popup (D67),
    // recreated - before the answer lands; one file input can be open at a
    // time, so a single slot is the entire state.
    private static ValueCallback<Uri[]> pendingFiles;

    /**
     * The setup page's folder button (wudict:folder-picker): the system's own
     * folder dialog, answering with a REAL PATH the exec'd server can read -
     * the one thing the web's showDirectoryPicker cannot name, and it does
     * not exist in a WebView at all. Distinct from Storage.REQ_TREE,
     * REQ_FILES and Intake.REQ_PICK: every result reaches all four handlers.
     */
    private static final int REQ_DIR = 0x5AF3;

    // The prompt the page is blocked on, the same single-slot bargain as
    // pendingFiles: one folder picker open at a time.
    private static JsPromptResult pendingDir;

    /**
     * Answers the folder prompt, exactly once: the picked path, or a cancel
     * when the user backed out or named something this shell cannot turn into
     * a path. What is NOT legal is silence - an unanswered prompt leaves the
     * page's JavaScript blocked forever.
     */
    private static void settleDir(String path) {
        JsPromptResult cb = pendingDir;
        pendingDir = null;
        if (cb == null) return;
        if (path != null) cb.confirm(path);
        else cb.cancel();
    }

    /**
     * Answers the page's file input, exactly once, with whatever we have.
     *
     * <p>Null is a legitimate answer and means "cancelled". What is NOT legal is
     * silence: an unanswered callback leaves that input permanently dead, so
     * every exit from the chooser path runs through here.
     */
    private static void settleFiles(Uri[] uris) {
        ValueCallback<Uri[]> cb = pendingFiles;
        pendingFiles = null;
        if (cb != null) cb.onReceiveValue(uris);
    }

    /**
     * The pickers' answers, forwarded by whichever activity hosts the WebView.
     * parseResult handles single, multiple (clipData) and cancel alike.
     */
    static void onActivityResult(Activity a, int requestCode, int resultCode, Intent data) {
        if (requestCode == REQ_FILES) {
            settleFiles(WebChromeClient.FileChooserParams.parseResult(resultCode, data));
            return;
        }
        if (requestCode != REQ_DIR) return;
        Uri tree = resultCode == Activity.RESULT_OK && data != null ? data.getData() : null;
        settleDir(tree == null ? null : treePath(a, tree));
    }

    /**
     * The folder a tree pick names, as a path on the filesystem, or null when
     * there is no such thing - the ordinary answer for a provider that is not
     * a plain volume (a cloud document, say). Only the externalstorage
     * provider is translated, by the same rule Intake.realPath applies to
     * files: the primary volume is Environment's directory, and any other
     * volume id IS its mount name under /storage. Tested as a DIRECTORY and
     * for readability, because a path the exec'd server cannot open would
     * only move the failure somewhere the user cannot see it answered.
     */
    private static String treePath(Context c, Uri tree) {
        if (!"content".equalsIgnoreCase(tree.getScheme())
                || !"com.android.externalstorage.documents".equals(tree.getAuthority())) {
            return null;
        }
        try {
            String id = DocumentsContract.getTreeDocumentId(tree); // "primary:Download"
            int cut = id.indexOf(':');
            if (cut <= 0) return null;
            String volume = id.substring(0, cut), rel = id.substring(cut + 1);
            if ("primary".equalsIgnoreCase(volume)) {
                String p = readableDir(new File(
                        Environment.getExternalStorageDirectory(), rel).getPath());
                if (p != null) return p;
            }
            // A microSD card, whose volume id IS its mount name.
            return readableDir("/storage/" + volume + "/" + rel);
        } catch (Exception e) {
            Log.w(TAG, "cannot resolve " + tree, e);
            return null;
        }
    }

    private static String readableDir(String p) {
        if (p == null) return null;
        File f = new File(p);
        return f.isDirectory() && f.canRead() ? f.getAbsolutePath() : null;
    }

    /** Answers window.open(): reads the URL the new window wants, then sends it out. */
    static WebChromeClient windows(Activity a) {
        return new WebChromeClient() {
            @Override
            public boolean onJsPrompt(WebView view, String url, String message,
                                      String defaultValue, android.webkit.JsPromptResult result) {
                if (url == null || !url.startsWith(origin(a) + "/")) return false;
                if ("wudict:dictionary-mode".equals(message)) {
                    if (!"all".equals(defaultValue) && !"found".equals(defaultValue)) {
                        result.cancel();
                        return true;
                    }
                    ShellPrefs.set(a, ShellPrefs.FOUND_DICTIONARIES, "found".equals(defaultValue));
                    result.confirm(defaultValue);
                    return true;
                }
                if ("wudict:appearance".equals(message)) {
                    try {
                        org.json.JSONObject request = new org.json.JSONObject(defaultValue);
                        String action = request.optString("action", "get");
                        if ("set".equals(action)) {
                            String field = request.getString("field");
                            // The window's own two rows, moved here from the
                            // settings screen: they change the SHELL's window,
                            // not the page, so they are stored here and applied
                            // through the activity that owns one. In the
                            // floating lookup window there is nothing to apply
                            // them to - a popup does not own the system bars -
                            // and the value simply awaits the app window.
                            boolean window = false;
                            if ("colorEnabled".equals(field)) {
                                ShellPrefs.of(a).edit().putBoolean(ShellPrefs.SEPIA,
                                        request.getBoolean("value")).apply();
                            } else if ("color".equals(field)) {
                                ShellPrefs.setSepiaColor(a, request.getString("value"));
                            } else if ("image".equals(field)) {
                                String name = request.getString("value");
                                if (!name.isEmpty() && !WindowBackground.images(a).contains(name))
                                    throw new IllegalArgumentException("Image unavailable");
                                ShellPrefs.of(a).edit().putString("background_image", name).apply();
                            } else if ("edgeMode".equals(field)) {
                                int mode = request.optInt("value", -1);
                                if (mode < ShellPrefs.EDGE_SYSTEM || mode > ShellPrefs.EDGE_NONE)
                                    throw new IllegalArgumentException("Unknown edge mode");
                                ShellPrefs.setEdgeMode(a, mode);
                                window = true;
                            } else if ("edgeColor".equals(field)) {
                                // parseColor takes #RGB, #RRGGBB and #AARRGGBB
                                // alike; the alpha is forced opaque on the way
                                // in, and a string it cannot read throws.
                                ShellPrefs.setEdgeColorValue(a,
                                        android.graphics.Color.parseColor(request.getString("value")));
                                window = true;
                            } else if ("bars".equals(field)) {
                                int mask = request.optInt("value", -1);
                                if (mask < 0 || mask > ShellPrefs.BARS_BOTH)
                                    throw new IllegalArgumentException("Unknown bar mask");
                                ShellPrefs.setBars(a, mask);
                                window = true;
                            } else throw new IllegalArgumentException("Unknown field");
                            if (window) {
                                if (a instanceof MainActivity) ((MainActivity) a).refreshScreen();
                            } else if (a instanceof MainActivity) {
                                ((MainActivity) a).refreshAppearance();
                            } else if (a instanceof LookupActivity) {
                                ((LookupActivity) a).refreshAppearance();
                            }
                        } else if (!"get".equals(action)) throw new IllegalArgumentException("Unknown action");
                        org.json.JSONObject reply = new org.json.JSONObject();
                        reply.put("colorEnabled", ShellPrefs.sepia(a));
                        reply.put("color", ShellPrefs.sepiaColorText(a));
                        reply.put("image", ShellPrefs.of(a).getString("background_image", ""));
                        org.json.JSONArray images = new org.json.JSONArray();
                        for (String name : WindowBackground.images(a)) images.put(name);
                        reply.put("images", images);
                        reply.put("edgeMode", ShellPrefs.edgeMode(a));
                        reply.put("edgeColor",
                                String.format("#%06X", 0xFFFFFF & ShellPrefs.edgeColorValue(a)));
                        reply.put("bars", ShellPrefs.bars(a));
                        result.confirm(reply.toString());
                    } catch (org.json.JSONException | IllegalArgumentException bad) {
                        result.cancel();
                    }
                    return true;
                }
                // The setup page's 📁. The page asks through the prompt it is
                // then blocked on; startActivityForResult answers it from
                // onActivityResult, so returning true here is "the answer
                // comes later", the same bargain DictionaryPicker makes.
                if ("wudict:folder-picker".equals(message)) {
                    // A second request supersedes the first; the abandoned
                    // prompt still gets its answer.
                    settleDir(null);
                    pendingDir = result;
                    Intent i = new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE)
                            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
                    try {
                        a.startActivityForResult(i, REQ_DIR);
                    } catch (ActivityNotFoundException | SecurityException e) {
                        Log.w(TAG, "no folder picker on this device", e);
                        pendingDir = null;
                        result.cancel();
                    }
                    return true;
                }
                if (!"wudict:dictionary-picker".equals(message)) return false;
                String pickerKind = "";
                try { pickerKind = new org.json.JSONObject(defaultValue).optString("kind"); }
                catch (org.json.JSONException ignored) { }
                if ("liveDict".equals(pickerKind)) {
                    DictionaryPicker.showLive(a, view, defaultValue, result);
                    return true;
                }
                if ("liveDictUpdate".equals(pickerKind)) {
                    DictionaryPicker.updateLive(view, defaultValue);
                    result.confirm("");
                    return true;
                }
                DictionaryPicker.show(a, defaultValue, result);
                return true;
            }

            @Override
            public boolean onCreateWindow(WebView view, boolean isDialog,
                                          boolean isUserGesture, Message resultMsg) {
                // window.open() has no URL in this callback - the only way to
                // learn it is to hand the transport a throwaway WebView and read
                // the navigation it is about to make.
                WebView sink = new WebView(view.getContext());
                sink.setWebViewClient(new WebViewClient() {
                    @Override
                    public boolean shouldOverrideUrlLoading(WebView v, WebResourceRequest req) {
                        openExternal(a, req.getUrl());
                        v.post(v::destroy); // never destroy a WebView inside its own callback
                        return true;
                    }
                });
                ((WebView.WebViewTransport) resultMsg.obj).setWebView(sink);
                resultMsg.sendToTarget();
                return true;
            }

            @Override
            @SuppressWarnings("deprecation") // startActivityForResult: no androidx here, by design
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback,
                                             FileChooserParams params) {
                // A second request supersedes the first; the abandoned one still
                // has to be answered or its input stays dead.
                settleFiles(null);
                pendingFiles = callback;
                try {
                    // createIntent() carries the accept= types and the multiple
                    // flag the page asked for, so the picker matches the markup.
                    a.startActivityForResult(params.createIntent(), REQ_FILES);
                } catch (ActivityNotFoundException | SecurityException e) {
                    Log.w(TAG, "no file picker on this device", e);
                    settleFiles(null);
                }
                // True either way: the callback is ours now, and it has already
                // been answered on the failure path.
                return true;
            }

            // The page's confirm() and alert() get the shared themed surface,
            // not the platform default: the Files list's "Delete X?" must sit
            // on the same wallpaper and palette as the pickers it appears
            // beside, or it reads as having left the app. An unanswered
            // JsResult holds the page's JavaScript, so every exit - buttons,
            // back, a window that could not be shown - answers it exactly once.
            @Override
            public boolean onJsConfirm(WebView view, String url, String message,
                                       JsResult result) {
                try {
                    new BackgroundDialogBuilder(a)
                            .setMessage(message)
                            .setPositiveButton(android.R.string.ok,
                                    (d, w) -> result.confirm())
                            .setNegativeButton(android.R.string.cancel,
                                    (d, w) -> result.cancel())
                            .setOnCancelListener(d -> result.cancel())
                            .show();
                } catch (RuntimeException badWindow) {
                    result.cancel();
                }
                return true;
            }

            @Override
            public boolean onJsAlert(WebView view, String url, String message,
                                     JsResult result) {
                try {
                    new BackgroundDialogBuilder(a)
                            .setMessage(message)
                            .setPositiveButton(android.R.string.ok,
                                    (d, w) -> result.confirm())
                            .setOnCancelListener(d -> result.cancel())
                            .show();
                } catch (RuntimeException badWindow) {
                    result.cancel();
                }
                return true;
            }
        };
    }
}
