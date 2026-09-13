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
import android.content.ClipData;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.provider.Settings;
import android.webkit.WebView;

import java.io.File;
import java.util.ArrayList;
import java.util.List;

final class Storage {

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
                new AlertDialog.Builder(a)
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

    /** No shell-private URLs in this flavour: the folder is reached directly. */
    static boolean handleShellUri(Activity a, Uri uri) {
        return false;
    }

    /** No import flow to receive a result for. */
    static void onActivityResult(Activity a, int requestCode, int resultCode, Intent data) {
    }

    /** Nothing to add to the page: the user manages the folder in a file manager. */
    static void onPageFinished(WebView web) {
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
