// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Archives arriving from outside the app: "Open with" from a file manager,
// "Share to" from a browser's download list, and the page's own
// wudict://intake channel.
//
// It is flavour-NEUTRAL, unlike Storage, and that is the point of the file.
// Nothing here is a permission, a picker grant or a folder policy - it hands
// the server either a path or a stream of bytes and lets internal/intake do
// the rest, which is the same code the desktop page drives. So the FOSS
// flavour, which had no intake of any kind, gets exactly what the Play
// flavour gets, and neither has an unarchiver, a format table or an idea of
// what a dictionary is made of (D52: the shell absorbs the platform, the Go
// side owns the dictionary).
//
// The division with Storage/SafImporter is by CARGO, not by flavour: an
// archive comes here, a loose dictionary file or a picked folder stays with
// Storage. Each manifest filter therefore has exactly one handler -
// MainActivity tries this one first and falls through.
package com.legbehindneck.wudict;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ContentResolver;
import android.content.Context;
import android.content.Intent;
import android.database.Cursor;
import android.net.Uri;
import android.os.Environment;
import android.provider.DocumentsContract;
import android.provider.MediaStore;
import android.provider.OpenableColumns;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.net.URLEncoder;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

final class Intake {

    private static final String TAG = "wudict";

    private static final int BUF = 1 << 20;             // 1 MiB, as SafImporter: these are multi-GB files
    private static final int CONNECT_MS = 5_000;
    // Covers spooling AND sniffing a multi-gigabyte archive inside one
    // request: the server answers POST /api/intake only once it has the whole
    // body on disk and has read the directory.
    private static final int READ_MS = 600_000;
    private static final int POLL_MS = 700;             // the page's own poll interval
    private static final long CHOICE_TIMEOUT_MIN = 10;  // a dialog nobody ever answers
    private static final int ERR_MAX = 8 << 10;         // an error body is one JSON object

    // One at a time here as well as in the server. The server's claim is the
    // real one; this one exists so a second share is answered immediately
    // instead of after a multi-gigabyte upload that is going to be refused.
    private static volatile boolean running;
    private static volatile boolean cancelled;

    /**
     * A link handed over by LookupActivity_sh, which is where a shared http(s)
     * URL lands: a browser shares a download link as text/plain, and that is
     * the filter the lookup popup owns. It recognises a link and forwards it
     * here rather than looking it up as a word.
     */
    static final String EXTRA_URL = "com.legbehindneck.wudict.extra.INTAKE_URL";

    // The picker the page's wudict://intake link opens. Distinct from
    // Storage.REQ_TREE and Shell.REQ_FILES: every result reaches all three.
    private static final int REQ_PICK = 0x5AF2;

    /**
     * The archive types this build can finish the job for - the same two the
     * manifest's VIEW and SEND filters claim, and the same two OpenArchive
     * dispatches on. One list, read by both the picker and the intent test, so
     * adding a format is one edit rather than three that can drift apart.
     */
    private static final String[] ARCHIVE_MIMES = {
            "application/zip",
            "application/x-zip-compressed",
            "application/x-7z-compressed",
    };

    /**
     * Loose dictionary files, by name. This is a PRE-FILTER, not a format
     * table: the server owns what a dictionary is made of and refuses
     * anything this lets through, and all this buys is not spooling a
     * gigabyte to be told no - which is a real cost now that the manifest
     * claims application/octet-stream and is therefore offered for untyped
     * files in general (D138).
     *
     * <p>Deliberately NARROWER than the server's rule, which also accepts a
     * companion that names its dictionary. It lists the main files plus
     * ".mdd", the second half somebody shares on its own after the first. The
     * two errors are not symmetric: too narrow is an absence the user can see
     * and route around with the page's own picker, too broad is a wasted
     * upload of somebody's disk image.
     */
    private static final String[] DICT_EXTS = {
            ".mdx", ".mdd", ".slob", ".bgl", ".zim", ".ifo", ".dsl", ".dsl.dz",
    };

    /**
     * What the page's own "Add dictionaries" picker offers. DocumentsUI
     * filters on EXTRA_MIME_TYPES, so this is the list that decides what is
     * selectable - and a loose .mdx has to be, or the picker is the one route
     * into intake that cannot take the commonest file there is. The two
     * generic types are what these formats are actually reported as; the
     * archive types are named as well so they keep sorting to the front.
     */
    private static final String[] PICK_MIMES = {
            "application/zip",
            "application/x-zip-compressed",
            "application/x-7z-compressed",
            "application/octet-stream",
            "application/gzip",
    };

    private Intake() {
    }

    // ── what arrives ─────────────────────────────────────────────────────

    /**
     * Takes an intent if it carries cargo this handles: an archive shared to
     * the app, or anything OPENED in a file manager. Returns false for what
     * is somebody else's - a picked folder, a text share, a loose file shared
     * rather than opened - so MainActivity can hand it to the flavour's
     * Storage unchanged.
     *
     * <p>The asymmetry between opening and sharing is the manifest's, not a
     * whim: the VIEW filter for these files lives in src/main and has one
     * handler, while the SEND filters for them are per-flavour and so are
     * their handlers (Play copies through SafImporter, FOSS routes back here
     * through startLoose). One filter, one handler, in every build.
     */
    static boolean onNewIntent(Activity a, Intent intent) {
        if (intent == null) return false;
        String url = intent.getStringExtra(EXTRA_URL);
        if (isURL(url)) {
            // Consumed, not merely read: MainActivity is singleTask and keeps
            // the intent that started it, so an activity recreated later would
            // otherwise start the same download a second time.
            intent.removeExtra(EXTRA_URL);
            startURL(a, url.trim());
            return true;
        }
        String action = intent.getAction();
        if (action == null) return false;
        if (Intent.ACTION_VIEW.equals(action)) return viewed(a, intent);
        List<Uri> uris = new ArrayList<>();
        if (Intent.ACTION_SEND.equals(action)) {
            Uri u = intent.getParcelableExtra(Intent.EXTRA_STREAM);
            if (u != null) uris.add(u);
        } else if (Intent.ACTION_SEND_MULTIPLE.equals(action)) {
            ArrayList<Uri> us = intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM);
            if (us != null) {
                for (Uri u : us) {
                    if (u != null) uris.add(u);
                }
            }
        } else {
            return false;
        }
        // Some senders put the payload in ClipData instead of EXTRA_STREAM.
        if (uris.isEmpty()) {
            ClipData clip = intent.getClipData();
            for (int i = 0; clip != null && i < clip.getItemCount(); i++) {
                Uri u = clip.getItemAt(i).getUri();
                if (u != null) uris.add(u);
            }
        }
        List<Uri> archives = new ArrayList<>();
        for (Uri u : uris) {
            if (isArchive(a, intent, u)) archives.add(u);
        }
        if (archives.isEmpty()) return false;
        start(a, archives);
        return true;
    }

    /**
     * The page→shell channel, beside Storage's own wudict:// hosts. It lives
     * here rather than in both flavours' Storage because an archive picker is
     * the same act in both.
     */
    static boolean handleShellUri(Activity a, Uri uri) {
        if (uri != null && "wudict".equalsIgnoreCase(uri.getScheme())
                && "intake".equalsIgnoreCase(uri.getHost())) {
            pick(a);
            return true;
        }
        return Storage.handleShellUri(a, uri);
    }

    @SuppressWarnings("deprecation") // startActivityForResult: no androidx here, by design
    private static void pick(Activity a) {
        // "*/*" plus EXTRA_MIME_TYPES rather than one setType: a picker shown a
        // single type filters to it exactly, and a .7z that a provider reports
        // as octet-stream - which several do - would then be unselectable. The
        // broad type keeps it reachable, the extra list keeps the archives at
        // the front, and the server answers for real when the bytes arrive.
        // PICK_MIMES and not ARCHIVE_MIMES: a loose .mdx is an import too, and
        // a picker that could not take one would be the only route into intake
        // that refuses the commonest file a user has (D138).
        Intent i = new Intent(Intent.ACTION_OPEN_DOCUMENT)
                .addCategory(Intent.CATEGORY_OPENABLE)
                .setType("*/*")
                .putExtra(Intent.EXTRA_MIME_TYPES, PICK_MIMES)
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        try {
            a.startActivityForResult(i, REQ_PICK);
        } catch (Exception e) {
            Log.w(TAG, "no document picker on this device", e);
            say(a, a.getString(R.string.intake_no_picker));
        }
    }

    static void onActivityResult(Activity a, int requestCode, int resultCode, Intent data) {
        if (requestCode != REQ_PICK) return;
        if (resultCode != Activity.RESULT_OK || data == null || data.getData() == null) return;
        List<Uri> one = new ArrayList<>();
        one.add(data.getData());
        start(a, one);
    }

    /**
     * Archive or not. The MIME type is what the intent system matched on and
     * is therefore the first answer; the name is the second, because a
     * file:// URI and several file managers report a plain octet-stream for a
     * .zip they are perfectly aware of.
     *
     * <p>zip and 7z only, matching the engine and the manifest filters: an
     * "archive" this build cannot open would be an entry in the user's
     * open-with sheet that only ever fails.
     */
    private static boolean isArchive(Context c, Intent intent, Uri u) {
        String mime = null;
        try {
            mime = c.getContentResolver().getType(u);
        } catch (Exception e) {
            Log.w(TAG, "no type for " + u, e);
        }
        if (mime == null && intent != null) mime = intent.getType();
        if (mime != null) {
            String m = mime.toLowerCase(Locale.US);
            for (String known : ARCHIVE_MIMES) {
                if (m.equals(known)) return true;
            }
        }
        String name = nameOf(c.getContentResolver(), u);
        if (name == null) return false;
        String n = name.toLowerCase(Locale.US);
        return n.endsWith(".zip") || n.endsWith(".7z");
    }

    /** Whether a name is one this app can begin an import from. See DICT_EXTS. */
    private static boolean isLooseDict(Context c, Uri u) {
        String name = nameOf(c.getContentResolver(), u);
        if (name == null) return false;
        String n = name.toLowerCase(Locale.US);
        for (String ext : DICT_EXTS) {
            if (n.endsWith(ext)) return true;
        }
        return false;
    }

    /**
     * Whether a piece of shared text is a link to fetch rather than a word to
     * look up. Deliberately shallow: which SITES may be downloaded from is the
     * server's setting (IMPORT_URL_HOSTS) and its refusal is a sentence the
     * user reads, so duplicating the list here would be a second policy to
     * keep in step with the first. All this decides is "link, or word".
     */
    static boolean isURL(String s) {
        if (s == null) return false;
        String v = s.trim();
        if (v.isEmpty() || v.indexOf(' ') >= 0) return false;
        String lower = v.toLowerCase(Locale.US);
        if (!lower.startsWith("http://") && !lower.startsWith("https://")) return false;
        try {
            return Uri.parse(v).getHost() != null;
        } catch (RuntimeException e) {
            return false;
        }
    }

    // ── the job ──────────────────────────────────────────────────────────

    /**
     * A file TAPPED in a file manager (D138). Always exactly one URI, and it
     * may be anything at all: the filter that offered us can only match on
     * MIME, every dictionary format arrives as application/octet-stream, and
     * so does every other untyped file on the device. The name is the first
     * point at which this app can tell them apart.
     *
     * <p>What it will not do is fall through silently. An app that appeared
     * in the sheet, was chosen, and then did nothing visible reads as broken;
     * the user ACTED, so the user is owed an answer (D102). The intent's data
     * is cleared with the answer because MainActivity is singleTask and keeps
     * the intent that started it - without that, a later recreation would say
     * the same sentence again about a file the user has long forgotten.
     */
    private static boolean viewed(Activity a, Intent intent) {
        Uri u = intent.getData();
        if (u == null) return false;
        String scheme = u.getScheme();
        if ("http".equalsIgnoreCase(scheme) || "https".equalsIgnoreCase(scheme)) {
            // A TAPPED link, from the web-link filter (D139). It must be
            // answered before the tests below, which ask the content resolver
            // for a display name: a web URI has no provider to ask, so both
            // would be false and a perfectly good download would be refused
            // by name. Cleared for the same singleTask reason as the decline
            // path; startURL outlives this call, so clearing first is safe.
            intent.setData(null);
            startURL(a, u.toString());
            return true;
        }
        if (isArchive(a, intent, u) || isLooseDict(a, u)) {
            List<Uri> one = new ArrayList<>();
            one.add(u);
            start(a, one);
            return true;
        }
        String name = nameOf(a.getContentResolver(), u);
        intent.setData(null);
        say(a, a.getString(R.string.intake_unsupported,
                name != null ? name : u.getLastPathSegment()));
        return true;
    }

    /**
     * Loose dictionary files SHARED to the app, routed here by a flavour's
     * Storage rather than claimed in onNewIntent - see the note there on why
     * that filter's handler is per-flavour. Anything in the list that is not
     * cargo is dropped without a word: unlike a tap, a multi-file share is
     * routinely a mixed bag, and naming each stray file would turn one act
     * into a row of complaints.
     */
    static void startLoose(Activity a, List<Uri> uris) {
        List<Uri> take = new ArrayList<>();
        for (Uri u : uris) {
            if (u != null && (isLooseDict(a, u) || isArchive(a, null, u))) take.add(u);
        }
        if (take.isEmpty()) {
            say(a, a.getString(R.string.intake_none));
            return;
        }
        start(a, take);
    }

    private static void start(Activity a, List<Uri> uris) {
        if (running) {
            say(a, a.getString(R.string.intake_busy));
            return;
        }
        running = true;
        cancelled = false;

        AlertDialog dialog = new AlertDialog.Builder(a)
                .setTitle(R.string.intake_title)
                .setMessage(a.getString(R.string.intake_reading, nameOf(a.getContentResolver(), uris.get(0))))
                .setCancelable(false)
                .setNegativeButton(R.string.intake_cancel, (d, w) -> cancelled = true)
                .create();
        dialog.show();

        // Held for exactly the reasons SafImporter holds it: this is minutes
        // of work in a process the platform reads as idle, and a kill strands
        // a half-extracted staging directory. The server retain is the second
        // half of the same guarantee - an import that outlives the activity
        // must not have its server killed out from under it by
        // MainActivity.onDestroy. release(true) hands that decision back.
        Context app = a.getApplicationContext();
        ServerProcess.retain();
        IndexService.hold(app, R.string.index_import_title, R.string.index_import_text);
        Thread t = new Thread(() -> {
            String summary;
            try {
                summary = run(a, app, uris, dialog);
            } catch (Exception e) {
                Log.w(TAG, "intake failed", e);
                summary = a.getString(R.string.intake_failed, String.valueOf(e.getMessage()));
            } finally {
                running = false;
                IndexService.release(app);
                ServerProcess.release(true);
            }
            final String msg = summary;
            a.runOnUiThread(() -> {
                if (a.isFinishing() || a.isDestroyed()) return;
                dialog.dismiss();
                if (a instanceof MainActivity) ((MainActivity) a).reloadPage();
                say(a, msg);
            });
        }, "wudict-intake");
        t.setDaemon(true);
        t.start();
    }

    /** Every archive in turn - the server takes one job at a time. */
    private static String run(Activity a, Context app, List<Uri> uris, AlertDialog dialog)
            throws Exception {
        if (!awaitServer(app)) return a.getString(R.string.intake_no_server);
        List<String> added = new ArrayList<>();
        String problem = null;
        for (Uri u : uris) {
            if (cancelled) break;
            String err = one(a, app, u, dialog, added);
            if (err != null && problem == null) problem = err;
        }
        if (problem != null && added.isEmpty()) return problem;
        if (cancelled && added.isEmpty()) return a.getString(R.string.intake_cancelled);
        if (added.isEmpty()) return a.getString(R.string.intake_none);
        String list = String.join(", ", added);
        return problem == null
                ? a.getString(R.string.intake_done, list)
                : a.getString(R.string.intake_done_partial, list, problem);
    }

    /**
     * A link. Same dialog, same single-job claim and same holds as an archive
     * share - only the acquisition differs, and it is the SERVER that does it:
     * the download outlives this activity, resumes a broken transfer, and is
     * not bound by the app's network security config (D52, D130).
     */
    private static void startURL(Activity a, String url) {
        if (running) {
            say(a, a.getString(R.string.intake_busy));
            return;
        }
        running = true;
        cancelled = false;

        AlertDialog dialog = new AlertDialog.Builder(a)
                .setTitle(R.string.intake_title)
                .setMessage(a.getString(R.string.intake_connecting))
                .setCancelable(false)
                .setNegativeButton(R.string.intake_cancel, (d, w) -> cancelled = true)
                .create();
        dialog.show();

        Context app = a.getApplicationContext();
        ServerProcess.retain();
        IndexService.hold(app, R.string.index_download_title, R.string.index_download_text);
        Thread t = new Thread(() -> {
            String summary;
            try {
                summary = runURL(a, app, url, dialog);
            } catch (Exception e) {
                Log.w(TAG, "download failed", e);
                summary = a.getString(R.string.intake_failed, String.valueOf(e.getMessage()));
            } finally {
                running = false;
                IndexService.release(app);
                ServerProcess.release(true);
            }
            final String msg = summary;
            a.runOnUiThread(() -> {
                if (a.isFinishing() || a.isDestroyed()) return;
                dialog.dismiss();
                if (a instanceof MainActivity) ((MainActivity) a).reloadPage();
                say(a, msg);
            });
        }, "wudict-intake-url");
        t.setDaemon(true);
        t.start();
    }

    private static String runURL(Activity a, Context app, String url, AlertDialog dialog)
            throws Exception {
        if (!awaitServer(app)) return a.getString(R.string.intake_no_server);
        JSONObject job = post(app, "/api/intake?url=" + enc(url), null, null);
        if (job.has("error")) return job.optString("error");

        job = awaitDownload(a, app, dialog);
        if (job == null) {
            return cancelled ? a.getString(R.string.intake_cancelled) : null;
        }
        if (job.has("error")) return job.optString("error");

        List<String> added = new ArrayList<>();
        String name = job.optString("source", url);
        String problem = offer(a, app, job, name, dialog, added);
        if (problem != null && added.isEmpty()) return problem;
        if (added.isEmpty()) {
            return cancelled ? a.getString(R.string.intake_cancelled)
                             : a.getString(R.string.intake_none);
        }
        String list = String.join(", ", added);
        return problem == null
                ? a.getString(R.string.intake_done, list)
                : a.getString(R.string.intake_done_partial, list, problem);
    }

    /**
     * Polls while the server fetches. Returns the sniffed job, null when the
     * download was cancelled or the job vanished, or a job carrying "error".
     *
     * <p>The percentage is absent when the site declared no length, because an
     * invented one that stops moving is worse than a line that only says work
     * is happening.
     */
    private static JSONObject awaitDownload(Activity a, Context app, AlertDialog dialog)
            throws Exception {
        for (; ; ) {
            if (cancelled) {
                // The partial file stays on the server's side on purpose: the
                // next attempt continues from it rather than starting over.
                post(app, "/api/intake", "DELETE", null);
                return null;
            }
            JSONObject j = post(app, "/api/intake", "GET", null);
            if (j.has("error")) return j;
            String state = j.optString("state");
            if ("ready".equals(state)) return j;
            if ("error".equals(state)) {
                JSONObject e = new JSONObject();
                e.put("error", j.optString("error", a.getString(R.string.intake_no_server)));
                return e;
            }
            if (!"downloading".equals(state)) return null; // cancelled elsewhere
            ui(a, dialog, downloadLine(a, j));
            mirror(app, j, true);
            Thread.sleep(POLL_MS);
        }
    }

    /** One archive: offer, confirm, extract. Returns an error message, or null. */
    private static String one(Activity a, Context app, Uri u, AlertDialog dialog,
                              List<String> added) throws Exception {
        String name = nameOf(a.getContentResolver(), u);
        if (name == null) name = "archive.zip";
        ui(a, dialog, a.getString(R.string.intake_reading, name));

        // Zero copy where the bytes are already a readable file: the server
        // reads the archive in place, and a 2 GB import costs nothing to
        // begin. Only the FOSS flavour normally gets here, since it is the one
        // holding all-files access - but the test is the file's readability,
        // not the flavour, so a file:// share reaches it in both.
        String path = realPath(a, u);
        JSONObject job = path != null
                ? post(app, "/api/intake?path=" + enc(path), null, null)
                : upload(a, app, u, name);
        if (job.has("error")) return job.optString("error");
        return offer(a, app, job, name, dialog, added);
    }

    /**
     * The half that is the same however the archive was acquired: show what is
     * in it, take the choice, install it. Shared by the share/open path and by
     * the download, so a link and a file are the same act from here on.
     */
    private static String offer(Activity a, Context app, JSONObject job, String name,
                                AlertDialog dialog, List<String> added) throws Exception {
        JSONArray cands = job.optJSONArray("candidates");
        if (cands == null || cands.length() == 0) {
            return a.getString(R.string.intake_nothing, name);
        }
        JSONArray extras = job.optJSONArray("extras");
        Choice chose = choose(a, name, cands, extras);
        if (chose == null) {
            cancelled = true;
            post(app, "/api/intake", "DELETE", null);
            return null;
        }
        if (chose.dicts.length == 0) {
            post(app, "/api/intake", "DELETE", null);
            return null;
        }

        JSONArray picked = new JSONArray();
        for (int i : chose.dicts) picked.put(i);
        JSONArray wanted = new JSONArray();
        for (int i : chose.extras) wanted.put(i);
        JSONObject body = new JSONObject();
        body.put("pick", picked);
        // The companions the user agreed to fetch. Sent even when empty, so a
        // confirm always says what was decided about them rather than leaving
        // the server to guess from an absent field.
        body.put("extras", wanted);
        // Always keep. The shell never deletes a file the user pointed at:
        // sharing an archive is not a request to lose it. A user who wants it
        // gone says so once, in IMPORT_KEEP, and the server honours that over
        // this (which is why sending true is not a lie - it is the request,
        // and the setting outranks it).
        body.put("keep", true);
        JSONObject started = post(app, "/api/intake?confirm=1", null, body.toString());
        if (started.has("error")) return started.optString("error");

        return await(a, app, dialog, added);
    }

    /** Polls until the install ends, moving the one progress line. */
    private static String await(Activity a, Context app, AlertDialog dialog, List<String> added)
            throws Exception {
        for (; ; ) {
            if (cancelled) {
                post(app, "/api/intake", "DELETE", null);
                return null;
            }
            JSONObject j = post(app, "/api/intake", "GET", null);
            String state = j.optString("state");
            if ("done".equals(state)) {
                JSONArray inst = j.optJSONArray("installed");
                for (int i = 0; inst != null && i < inst.length(); i++) {
                    added.add(inst.optString(i));
                }
                return null;
            }
            if ("error".equals(state)) return j.optString("error");
            // A confirmed job with companions ticked goes back to DOWNLOADING
            // before it installs: the extras are fetched first, then the set is
            // re-sniffed with them in it. Reading that as "cancelled elsewhere"
            // is what once made the shell report "nothing was added" over an
            // import that was still running and went on to succeed.
            if ("downloading".equals(state)) {
                ui(a, dialog, downloadLine(a, j));
                mirror(app, j, true);
                Thread.sleep(POLL_MS);
                continue;
            }
            if (!"installing".equals(state)) return null; // cancelled elsewhere
            long done = j.optLong("done"), total = j.optLong("total");
            int pct = total > 0 ? (int) Math.min(100, done * 100 / total) : 0;
            ui(a, dialog, a.getString(R.string.intake_installing, pct));
            mirror(app, j, false);
            Thread.sleep(POLL_MS);
        }
    }

    /**
     * The one progress line a download gets, wherever in the job it happens:
     * the file the site named, and the site. The file is empty until the
     * server has answered - the name is its decision, through a redirect or a
     * Content-Disposition - and then the host alone is the only honest line.
     * Named because a job fetches the dictionary and then each companion the
     * user ticked, and a line that said only the host could not tell those
     * apart.
     */
    /**
     * The notification says what the dialog says. It is the same progress, in
     * the one place the user can still see it after leaving the app - and
     * leaving the app is the normal thing to do during a gigabyte download
     * (D140). Called on every poll tick; the service repaints only on a change.
     */
    private static void mirror(Context app, JSONObject j, boolean downloading) {
        if (downloading) {
            IndexService.phase(app, R.string.index_download_title, R.string.index_download_text);
        } else {
            IndexService.phase(app, R.string.index_import_title, R.string.index_import_text);
        }
        long done = j.optLong("done"), total = j.optLong("total");
        IndexService.progress(app, total > 0 ? (int) Math.min(100, done * 100 / total) : -1);
    }

    private static String downloadLine(Activity a, JSONObject j) {
        long done = j.optLong("done"), total = j.optLong("total");
        String file = j.optString("source", "");
        String host = j.optString("host", "");
        // No percentage when the site declared no length: an invented one that
        // stops moving is worse than a line that only says work is happening.
        int pct = total > 0 ? (int) Math.min(100, done * 100 / total) : -1;
        if (file.isEmpty()) {
            return pct >= 0
                    ? a.getString(R.string.intake_downloading_pct, host, pct)
                    : a.getString(R.string.intake_downloading, host);
        }
        return pct >= 0
                ? a.getString(R.string.intake_downloading_named_pct, file, host, pct)
                : a.getString(R.string.intake_downloading_named, file, host);
    }

    /**
     * What to install. Incomplete candidates are LISTED - so the user sees
     * that the archive held one and what it is missing - and cannot be
     * checked: the server refuses them anyway, and silently dropping a box
     * the user ticked is worse than never letting it tick.
     *
     * A dictionary the library already holds says so, and one that is
     * BYTE-FOR-BYTE the same starts unticked: the usual reason to be looking
     * at this list again is the other dictionary in the same archive. Ticking
     * it anyway replaces it, which is what somebody repairing a folder wants.
     * There is no "add a second copy" here on purpose - the shell offers what
     * must be decided, and wanting two of one dictionary is a choice for the
     * page, not for a dialog somebody reached by sharing a file (D102, D134).
     */
    private static Choice choose(Activity a, String archive, JSONArray cands, JSONArray extras)
            throws Exception {
        final int n = cands.length();
        final int e = extras == null ? 0 : extras.length();
        String[] labels = new String[n + e];
        final boolean[] checked = new boolean[n + e];
        final boolean[] ok = new boolean[n + e];
        // For each candidate, the extras rows that carry the files it cannot
        // work without. Untick one of those and the dictionary it belongs to
        // stops being installable, so the row unticks with it rather than
        // producing a refusal from the server two taps later.
        final List<List<Integer>> needs = new ArrayList<>();
        for (int i = 0; i < n; i++) {
            JSONObject c = cands.getJSONObject(i);
            String label = row(a, c, true);
            JSONArray missing = c.optJSONArray("missing");
            boolean complete = missing == null || missing.length() == 0;
            List<Integer> dep = new ArrayList<>();
            boolean same = false;
            if (!complete) {
                StringBuilder b = new StringBuilder();
                for (int k = 0; k < missing.length(); k++) {
                    if (k > 0) b.append(", ");
                    b.append(missing.optString(k));
                    int at = supplier(extras, missing.optString(k));
                    if (at >= 0) dep.add(n + at);
                }
                // Missing from the archive, but found beside the file it was
                // downloaded from and about to be fetched: what the user is
                // choosing is the state AFTER the companions, and the server
                // re-checks once they have landed, so a download that fails
                // still refuses rather than installing half a dictionary.
                boolean soon = dep.size() == missing.length();
                ok[i] = soon;
                // Without the total: a set that cannot be installed yet is not
                // a download to weigh, and the clause saying what it lacks is
                // what the row is there to carry.
                label = soon
                        ? a.getString(R.string.intake_needs_found, row(a, c, false), b.toString())
                        : a.getString(R.string.intake_needs, row(a, c, false), b.toString());
            } else {
                ok[i] = true;
                if (!c.optString("existing").isEmpty()) {
                    same = c.optBoolean("unchanged");
                    // A folder holding no dictionary keeps its total: nothing
                    // of this is on the phone yet, so the download is still the
                    // decision. The other two drop it - those bytes are already
                    // here, and what is being decided is whether to replace
                    // them.
                    boolean stale = c.optBoolean("stale");
                    int msg = stale ? R.string.intake_stale
                            : same ? R.string.intake_same : R.string.intake_update;
                    label = a.getString(msg, row(a, c, stale));
                }
            }
            needs.add(dep);
            labels[i] = label;
            checked[i] = ok[i] && !same;
        }
        for (int k = 0; k < e; k++) {
            JSONObject x = extras.getJSONObject(k);
            String why = a.getString(gradeString(x.optString("need")));
            long size = x.optLong("size");
            labels[n + k] = size > 0
                    ? a.getString(R.string.intake_extra, x.optString("name"), human(size), why)
                    : a.getString(R.string.intake_extra_nosize, x.optString("name"), why);
            // Ticked, because a dictionary without its media has no images and
            // no sound. Ticked and not fetched, because on a phone this is
            // gigabytes of somebody's data allowance and the size is the whole
            // decision - which is why it is on the screen at all (D102).
            ok[n + k] = true;
            checked[n + k] = true;
        }

        final Choice[] answer = new Choice[1];
        final CountDownLatch latch = new CountDownLatch(1);
        a.runOnUiThread(() -> {
            if (a.isFinishing() || a.isDestroyed()) {
                latch.countDown(); // answer stays null: treated as a cancel
                return;
            }
            AlertDialog d = new AlertDialog.Builder(a)
                    .setTitle(a.getString(R.string.intake_add_from, archive))
                    .setMultiChoiceItems(labels, checked, (dlg, which, isChecked) -> {
                        if (isChecked && !ok[which]) {
                            checked[which] = false;
                            ((AlertDialog) dlg).getListView().setItemChecked(which, false);
                            return;
                        }
                        checked[which] = isChecked;
                        if (isChecked || which < n) return;
                        // A required companion just came off: every dictionary
                        // that was only installable because of it comes off
                        // too, on screen, rather than being refused later.
                        for (int i = 0; i < n; i++) {
                            if (checked[i] && needs.get(i).contains(which)) {
                                checked[i] = false;
                                ((AlertDialog) dlg).getListView().setItemChecked(i, false);
                            }
                        }
                    })
                    .setCancelable(false)
                    .setPositiveButton(R.string.intake_install, (dlg, w) -> {
                        List<Integer> dicts = new ArrayList<>(), want = new ArrayList<>();
                        for (int i = 0; i < checked.length; i++) {
                            if (!checked[i] || !ok[i]) continue;
                            if (i < n) dicts.add(i);
                            else want.add(i - n);
                        }
                        answer[0] = new Choice(ints(dicts), ints(want));
                        latch.countDown();
                    })
                    .setNegativeButton(R.string.intake_cancel, (dlg, w) -> latch.countDown())
                    .create();
            d.show();
        });
        // A dialog whose window went away with the activity would otherwise
        // park this thread - and its server claim - for the life of the app.
        if (!latch.await(CHOICE_TIMEOUT_MIN, TimeUnit.MINUTES)) return null;
        return answer[0];
    }

    /**
     * One dictionary as one line: what it is called, which files it is made
     * of, and - when there is a download to weigh - what they come to.
     *
     * The files are there because the name on its own is a title, and a title
     * does not say which file is about to be downloaded, replaced or deleted.
     * "OED" is the same word whether it arrived as a 200 MB .mdx or as that
     * plus a 1.2 GB .mdd, and on a phone those are not the same decision
     * (D137).
     *
     * withSize is false wherever the row already carries a state - already
     * installed, or missing a companion - for two reasons that agree: those
     * bytes are not a cost the user is about to pay, and this dialog's rows
     * are one line that ellipsizes, so the clause that is the decision must
     * not be pushed off the end by a number that is not.
     */
    private static String row(Activity a, JSONObject c, boolean withSize) {
        String name = c.optString("name");
        String files = fileList(a, c.optJSONArray("files"));
        long size = withSize ? c.optLong("size") : 0;
        if (files.isEmpty()) {
            return size > 0 ? a.getString(R.string.intake_cand_short, name, human(size)) : name;
        }
        return size > 0
                ? a.getString(R.string.intake_cand, name, files, human(size))
                : a.getString(R.string.intake_cand_short, name, files);
    }

    /** How many file names a row shows before it starts counting instead. */
    private static final int FILES_SHOWN = 3;

    /**
     * The files, joined. Cut at FILES_SHOWN because a StarDict set is four
     * files and a resource folder, and a row that grows with the dictionary
     * stops being readable at exactly the point it matters most.
     */
    private static String fileList(Activity a, JSONArray files) {
        if (files == null || files.length() == 0) return "";
        int show = Math.min(files.length(), FILES_SHOWN);
        StringBuilder b = new StringBuilder();
        for (int i = 0; i < show; i++) {
            if (i > 0) b.append(" + ");
            b.append(files.optString(i));
        }
        if (files.length() > show) {
            b.append(" + ").append(a.getString(R.string.intake_files_more, files.length() - show));
        }
        return b.toString();
    }

    /**
     * What the one dialog decided: which dictionaries to install, and which of
     * the companion files found beside the download to fetch. Two lists rather
     * than one, because the server takes them as two - the indexes are into
     * different arrays, and merging them here only to split them again there
     * is how an off-by-one becomes a wrong file.
     */
    private static final class Choice {
        final int[] dicts;
        final int[] extras;

        Choice(int[] dicts, int[] extras) {
            this.dicts = dicts;
            this.extras = extras;
        }
    }

    private static int[] ints(List<Integer> v) {
        int[] out = new int[v.size()];
        for (int i = 0; i < out.length; i++) out[i] = v.get(i);
        return out;
    }

    /**
     * The offered companion that would supply the missing file suffix, or -1.
     * Matched by suffix because that is what the server reports missing - an
     * extension such as ".idx" - against the whole name of a file, which is
     * the dictionary's stem plus exactly that.
     */
    private static int supplier(JSONArray extras, String missing) {
        if (extras == null || missing == null || missing.isEmpty()) return -1;
        String want = missing.toLowerCase(Locale.US);
        for (int i = 0; i < extras.length(); i++) {
            JSONObject x = extras.optJSONObject(i);
            if (x == null || !"needed".equals(x.optString("need"))) continue;
            if (x.optString("name").toLowerCase(Locale.US).endsWith(want)) return i;
        }
        return -1;
    }

    /** Why a companion is worth its bytes, in the three grades the server has. */
    private static int gradeString(String need) {
        if ("needed".equals(need)) return R.string.intake_extra_needed;
        if ("media".equals(need)) return R.string.intake_extra_media;
        return R.string.intake_extra_other;
    }

    /**
     * A size somebody can decide on. Binary units, one decimal below ten, and
     * never a bare byte count: "1.4 GB" is the sentence that stops a download
     * on mobile data, and "1503238553" is not.
     */
    private static String human(long n) {
        if (n <= 0) return "";
        final String[] unit = {"B", "KB", "MB", "GB", "TB"};
        double v = n;
        int i = 0;
        while (v >= 1024 && i < unit.length - 1) {
            v /= 1024;
            i++;
        }
        return (i > 0 && v < 10 ? String.format(Locale.US, "%.1f", v)
                                : String.valueOf(Math.round(v))) + " " + unit[i];
    }

    // ── talking to the server ────────────────────────────────────────────

    private static boolean awaitServer(Context app) throws InterruptedException {
        final boolean[] ok = new boolean[1];
        final CountDownLatch latch = new CountDownLatch(1);
        ServerProcess.ensure(app, new ServerProcess.Listener() {
            @Override
            public void onReady() {
                ok[0] = true;
                latch.countDown();
            }

            @Override
            public void onFailed(String message) {
                Log.w(TAG, "server not available for intake: " + message);
                latch.countDown();
            }
        });
        latch.await(90, TimeUnit.SECONDS);
        return ok[0];
    }

    /**
     * One request, one JSON object. An error - HTTP or transport - comes back
     * as an object carrying "error", so every caller reads the outcome in one
     * place instead of catching around each call.
     */
    private static JSONObject post(Context c, String path, String method, String body) {
        HttpURLConnection h = null;
        try {
            h = (HttpURLConnection) new URL(Shell.origin(c) + path).openConnection();
            h.setRequestMethod(method == null ? "POST" : method);
            h.setConnectTimeout(CONNECT_MS);
            h.setReadTimeout(READ_MS);
            ShellPrefs.authorize(h);
            if (body != null) {
                byte[] b = body.getBytes("UTF-8");
                h.setDoOutput(true);
                h.setFixedLengthStreamingMode(b.length);
                h.setRequestProperty("Content-Type", "application/json");
                try (OutputStream out = h.getOutputStream()) {
                    out.write(b);
                }
            }
            return read(h);
        } catch (Exception e) {
            Log.w(TAG, "intake request failed: " + path, e);
            return error(String.valueOf(e.getMessage()));
        } finally {
            if (h != null) h.disconnect();
        }
    }

    /**
     * The copying path: the bytes cross the process boundary because a
     * content:// URI is a token the exec'd server can never resolve (D62). The
     * stream is written straight through to the request - never held - and the
     * server spools it inside the destination folder, so the install that
     * follows is a rename rather than a second full copy.
     */
    private static JSONObject upload(Context ui, Context app, Uri u, String name) {
        ContentResolver cr = ui.getContentResolver();
        long size = sizeOf(cr, u);
        HttpURLConnection h = null;
        try (InputStream in = cr.openInputStream(u)) {
            if (in == null) return error("cannot read that file");
            h = (HttpURLConnection) new URL(Shell.origin(app) + "/api/intake?name=" + enc(name))
                    .openConnection();
            h.setRequestMethod("POST");
            h.setConnectTimeout(CONNECT_MS);
            h.setReadTimeout(READ_MS);
            ShellPrefs.authorize(h);
            h.setDoOutput(true);
            h.setRequestProperty("Content-Type", "application/octet-stream");
            // Known length where the provider gives one, so the server sees a
            // Content-Length; chunked otherwise. Either way the body is
            // streamed: setChunkedStreamingMode is what keeps HttpURLConnection
            // from buffering the whole request in the heap first.
            if (size > 0) {
                h.setFixedLengthStreamingMode(size);
            } else {
                h.setChunkedStreamingMode(BUF);
            }
            try (OutputStream out = h.getOutputStream()) {
                byte[] buf = new byte[BUF];
                for (int n; (n = in.read(buf)) > 0; ) {
                    if (cancelled) return error(null);
                    out.write(buf, 0, n);
                }
            }
            return read(h);
        } catch (Exception e) {
            Log.w(TAG, "upload failed: " + u, e);
            return error(String.valueOf(e.getMessage()));
        } finally {
            if (h != null) h.disconnect();
        }
    }

    private static JSONObject read(HttpURLConnection h) throws IOException {
        int code = h.getResponseCode();
        InputStream in = code >= 400 ? h.getErrorStream() : h.getInputStream();
        String text = slurp(in);
        try {
            JSONObject o = new JSONObject(text);
            if (code >= 400 && !o.has("error")) return error("HTTP " + code);
            return o;
        } catch (Exception e) {
            return error(code >= 400 ? "HTTP " + code : "unreadable answer from the server");
        }
    }

    private static String slurp(InputStream in) throws IOException {
        if (in == null) return "";
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        byte[] buf = new byte[4096];
        for (int n; out.size() < ERR_MAX && (n = in.read(buf)) > 0; ) {
            out.write(buf, 0, n);
        }
        return out.toString("UTF-8");
    }

    private static JSONObject error(String message) {
        JSONObject o = new JSONObject();
        try {
            o.put("error", message == null ? "" : message);
        } catch (Exception ignored) {
            // JSONObject.put only throws on a null key
        }
        return o;
    }

    private static String enc(String s) {
        try {
            return URLEncoder.encode(s, "UTF-8");
        } catch (Exception e) {
            return ""; // UTF-8 is guaranteed by the platform
        }
    }

    // ── the bytes, where they already are ────────────────────────────────

    /**
     * The archive as a path the server can open, or null when there is none -
     * which is the ordinary answer for a Storage Access Framework document and
     * for every provider that is not a file at all.
     *
     * <p>Readability is the whole test, and it is deliberately not "is this
     * the FOSS flavour": all-files access is what usually makes it true, but a
     * file:// share and the app's own external files dir are readable without
     * it, and a flavour check would refuse those for no reason.
     */
    static String realPath(Context c, Uri u) {
        String scheme = u.getScheme();
        if (scheme == null || "file".equalsIgnoreCase(scheme)) {
            return readable(u.getPath());
        }
        if (!"content".equalsIgnoreCase(scheme)) return null;
        try {
            if ("com.android.externalstorage.documents".equals(u.getAuthority())
                    && DocumentsContract.isDocumentUri(c, u)) {
                String id = DocumentsContract.getDocumentId(u); // "primary:Download/x.zip"
                int cut = id.indexOf(':');
                if (cut > 0) {
                    String volume = id.substring(0, cut), rel = id.substring(cut + 1);
                    if ("primary".equalsIgnoreCase(volume)) {
                        String p = readable(new File(
                                Environment.getExternalStorageDirectory(), rel).getPath());
                        if (p != null) return p;
                    }
                    // A microSD card, whose volume id IS its mount name.
                    String p = readable("/storage/" + volume + "/" + rel);
                    if (p != null) return p;
                }
            }
        } catch (Exception e) {
            Log.w(TAG, "cannot resolve " + u, e);
        }
        // MediaStore still reports a real path for files on primary storage.
        // Deprecated, and asked for last precisely because it is: the answer
        // is only ever used after canRead() agrees with it.
        try (Cursor cur = c.getContentResolver().query(
                u, new String[]{MediaStore.MediaColumns.DATA}, null, null, null)) {
            if (cur != null && cur.moveToFirst() && !cur.isNull(0)) return readable(cur.getString(0));
        } catch (Exception e) {
            Log.w(TAG, "no path column for " + u, e);
        }
        return null;
    }

    private static String readable(String p) {
        if (p == null) return null;
        File f = new File(p);
        return f.isFile() && f.canRead() ? f.getAbsolutePath() : null;
    }

    // ── small change ─────────────────────────────────────────────────────

    private static String nameOf(ContentResolver cr, Uri u) {
        try (Cursor c = cr.query(u, new String[]{OpenableColumns.DISPLAY_NAME},
                null, null, null)) {
            if (c != null && c.moveToFirst() && !c.isNull(0)) return c.getString(0);
        } catch (Exception e) {
            Log.w(TAG, "no display name for " + u, e);
        }
        String last = u.getLastPathSegment();
        if (last == null) return null;
        int cut = last.lastIndexOf('/');
        return cut >= 0 ? last.substring(cut + 1) : last;
    }

    /** The declared size, or 0 when the provider will not say. */
    private static long sizeOf(ContentResolver cr, Uri u) {
        try (Cursor c = cr.query(u, new String[]{OpenableColumns.SIZE}, null, null, null)) {
            if (c != null && c.moveToFirst() && !c.isNull(0)) return c.getLong(0);
        } catch (Exception e) {
            Log.w(TAG, "no size for " + u, e);
        }
        return 0;
    }

    private static void ui(Activity a, AlertDialog dialog, String message) {
        a.runOnUiThread(() -> {
            if (!a.isFinishing() && !a.isDestroyed()) dialog.setMessage(message);
        });
    }

    private static void say(Activity a, String message) {
        if (message == null || a.isFinishing() || a.isDestroyed()) return;
        new AlertDialog.Builder(a)
                .setMessage(message)
                .setPositiveButton(android.R.string.ok, null)
                .show();
    }
}
