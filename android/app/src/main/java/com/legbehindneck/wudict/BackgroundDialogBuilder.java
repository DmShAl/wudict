package com.legbehindneck.wudict;

import android.app.AlertDialog;
import android.content.Context;

/** Shared surface for app-owned dialogs, including those shown via create(). */
final class BackgroundDialogBuilder extends AlertDialog.Builder {
    private final Context context;

    BackgroundDialogBuilder(Context context) {
        super(context, ShellPrefs.darkIcons(ShellPrefs.pageBg(context))
                ? android.R.style.Theme_Material_Light_Dialog_Alert
                : android.R.style.Theme_Material_Dialog_Alert);
        this.context = context;
    }

    @Override public AlertDialog create() {
        AlertDialog dialog = super.create();
        dialog.getWindow().setBackgroundDrawable(
                WindowBackground.dialogDrawable(context, ShellPrefs.pageBg(context)));
        return dialog;
    }
}
