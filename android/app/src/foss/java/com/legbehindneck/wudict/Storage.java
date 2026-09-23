// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

// The FOSS flavour's storage policy (D62): the shared "Dictionaries" folder,
// reached with All-files access, exactly as D52 shipped it.
//
// One class of this name exists per flavour and NEVER in src/main, so the
// Play build cannot compile - or even name - the intents and permissions
// below. That is the whole point of the split: the shell calls Storage, and
// what Storage is depends on which APK is being built.
package com.legbehindneck.wudict;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ActivityNotFoundException;
import android.content.ClipData;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.provider.DocumentsContract;
import android.provider.Settings;
import android.util.Log;
import android.webkit.WebView;

import org.json.JSONObject;

import java.io.File;
import java.lang.ref.WeakReference;
import java.util.ArrayList;
import java.util.List;

final class Storage {

    private static final String TAG = "wudict";

    // The folder dialog behind the setup page's 📁. Distinct from Shell's
    // REQ_FILES and Intake's REQ_PICK: every result reaches all three.
    private static final int REQ_DIR = 0x5AF3;

    // The page to answer when the dialog returns. Weak: the activity owns the
    // WebView, and a result after it is gone has no one to tell.
    private static WeakReference<WebView> page = new WeakReference<>(null);

    // The page side of the dialog (D54): a capability, not a platform. The
    // setup page shows 📁 only while data-folder-picker is set, and learns
    // nothing about who set it. wudict://folder opens the dialog; the path
    // comes back through __wdFolderPicked to the one row that asked. Only this
    // flavour offers it - its all-files access is what makes a path worth
    // typing; the Play flavour's typed paths are unreadable, so it never
    // defines the hook and the button never appears there.
    private static final String FOLDER_PICKER_JS =
            "(function(){if(window.wudictPickFolder)return;var done=null;"
                    + "window.wudictPickFolder=function(f){done=f;location.href='wudict://folder';};"
                    + "window.__wdFolderPicked=function(p){var f=done;done=null;if(f)f(p);};"
                    + "document.documentElement.setAttribute('data-folder-picker','');})()";

    private Storage() {
    }

    /**
     * Folders seeded into DICT_DIR, in order: the shared folder the user drops
     * files into, then the app-owned one that needs no permission at all.
     */
    static File[] dictDirs(android.content.Context c) {
        File shared = new File(Environment.getExternalStorageDirectory(), "Dictionaries");
        shared.mkdirs(); // best effort: needs the storage grant on API 30+
        return new File[]{shared, AppDirs.appDicts(c)};
    }

    /**
     * Asks for the storage grant. Either way the server starts - it reports
     * the folder as empty until files arrive.
     */
    static void ensureAccess(Activity a) {
        if (Build.VERSION.SDK_INT >= 30) {
            if (!Environment.isExternalStorageManager()) {
                new BackgroundDialogBuilder(a)
                        .setTitle(R.string.storage_title)
                        .setMessage(R.string.storage_message)
                        .setPositiveButton(R.string.storage_grant, (dialog, which) ->
                                a.startActivity(new Intent(
                                        Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION,
                                        Uri.parse("package:" + a.getPackageName()))))
                        .setNegativeButton(R.string.storage_later, null)
                        .show();
            }
        } else if (a.checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE)
                != PackageManager.PERMISSION_GRANTED) {
            a.requestPermissions(
                    new String[]{Manifest.permission.WRITE_EXTERNAL_STORAGE}, 1);
        }
    }

    /**
     * wudict://folder is the setup page's 📁. Anything else is not this
     * flavour's, and goes on to the browser as before.
     */
    static boolean handleShellUri(Activity a, Uri uri) {
        if (uri != null && "wudict".equalsIgnoreCase(uri.getScheme())
                && "folder".equalsIgnoreCase(uri.getHost())) {
            pickFolder(a);
            return true;
        }
        return false;
    }

    /** The folder dialog's answer, handed to the page as a path. */
    static void onActivityResult(Activity a, int requestCode, int resultCode, Intent data) {
        if (requestCode != REQ_DIR || resultCode != Activity.RESULT_OK || data == null) return;
        String path = treePath(data.getData());
        WebView web = page.get();
        if (path == null || web == null || !web.isAttachedToWindow()) return;
        web.evaluateJavascript("window.__wdFolderPicked&&window.__wdFolderPicked("
                + JSONObject.quote(path) + ")", null);
    }

    /** Offers the folder dialog to the page; the user manages files in a file manager. */
    static void onPageFinished(WebView web) {
        page = new WeakReference<>(web);
        web.evaluateJavascript(FOLDER_PICKER_JS, null);
    }

    @SuppressWarnings("deprecation") // startActivityForResult: no androidx here, by design
    private static void pickFolder(Activity a) {
        // No grant flags: nothing is read through the tree URI. It is only
        // translated to the path it names, which all-files access then reads.
        try {
            a.startActivityForResult(new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE), REQ_DIR);
        } catch (ActivityNotFoundException | SecurityException e) {
            Log.w(TAG, "no folder picker on this device", e);
        }
    }

    /**
     * The filesystem path behind a tree the external-storage provider chose,
     * or null for any other provider (Drive, Downloads' virtual roots, ...):
     * those name no path the server could open. A tree id is "volume:relative"
     * - "primary" the shared storage, "home" its Documents folder, anything
     * else a removable volume mounted at /storage/<id>. Whether the folder is
     * readable is not checked here: the setup page validates every row and
     * says so beside it.
     */
    static String treePath(Uri tree) {
        if (tree == null || !"com.android.externalstorage.documents".equals(tree.getAuthority())) {
            return null;
        }
        String id;
        try {
            id = DocumentsContract.getTreeDocumentId(tree);
        } catch (IllegalArgumentException e) {
            return null;
        }
        int cut = id == null ? -1 : id.indexOf(':');
        if (cut <= 0) return null;
        String vol = id.substring(0, cut), rel = id.substring(cut + 1);
        File base;
        if ("primary".equalsIgnoreCase(vol)) {
            base = Environment.getExternalStorageDirectory();
        } else if ("home".equalsIgnoreCase(vol)) {
            base = new File(Environment.getExternalStorageDirectory(), "Documents");
        } else {
            base = new File("/storage", vol);
        }
        return rel.isEmpty() ? base.getPath() : new File(base, rel).getPath();
    }

    /**
     * A loose dictionary file shared to this app (D138). The archive types are
     * claimed in src/main and handled there; these filters live in this
     * flavour's manifest because their handler does - the Play flavour copies
     * shared documents through SafImporter, and this one has all-files access,
     * so the server can read the file where it lies and pick up the .mdd
     * sitting beside the .mdx on its own.
     *
     * <p>Which is why this routes to Intake rather than growing an importer of
     * its own: the work is identical to a tap, and the only thing this flavour
     * contributes is the permission that makes the file readable.
     */
    static void onNewIntent(Activity a, Intent intent) {
        if (intent == null) return;
        String action = intent.getAction();
        if (!Intent.ACTION_SEND.equals(action)
                && !Intent.ACTION_SEND_MULTIPLE.equals(action)) {
            return;
        }
        List<Uri> docs = new ArrayList<>();
        if (Intent.ACTION_SEND.equals(action)) {
            Uri u = intent.getParcelableExtra(Intent.EXTRA_STREAM);
            if (u != null) docs.add(u);
        } else {
            ArrayList<Uri> us = intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM);
            if (us != null) {
                for (Uri u : us) {
                    if (u != null) docs.add(u);
                }
            }
        }
        // Some senders put the payload in ClipData instead of EXTRA_STREAM.
        if (docs.isEmpty()) {
            ClipData clip = intent.getClipData();
            for (int i = 0; clip != null && i < clip.getItemCount(); i++) {
                Uri u = clip.getItemAt(i).getUri();
                if (u != null) docs.add(u);
            }
        }
        if (!docs.isEmpty()) Intake.startLoose(a, docs);
    }
}
