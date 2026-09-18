package com.legbehindneck.wudict;

import android.app.Activity;
import android.app.AlertDialog;
import android.graphics.Typeface;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.JsPromptResult;
import android.widget.ArrayAdapter;
import android.widget.TextView;
import org.json.JSONArray;
import org.json.JSONObject;
import org.json.JSONException;

/** A themed replacement for WebView's dictionary select popup. */
final class DictionaryPicker {
    static void show(Activity activity, String payload, JsPromptResult result) {
        if (activity.isFinishing() || activity.isDestroyed()) { result.cancel(); return; }
        try {
            JSONObject data = new JSONObject(payload);
            boolean found = data.optBoolean("found");
            boolean examples = "stylerPreset".equals(data.optString("kind"));
            boolean plain = found || examples;
            JSONArray rows = data.getJSONArray("rows");
            String[] labels = new String[rows.length()];
            int[] indices = new int[rows.length()];
            boolean[] disabled = new boolean[rows.length()];
            int checked = -1;
            for (int i = 0; i < rows.length(); i++) {
                JSONObject row = rows.getJSONObject(i);
                labels[i] = row.getString("label");
                indices[i] = row.getInt("index");
                disabled[i] = row.optBoolean("disabled") || indices[i] < 0;
                if (indices[i] >= 0 && indices[i] == data.optInt("selected", -1)) checked = i;
            }
            int color = ShellPrefs.pageBg(activity);
            int textColor = ShellPrefs.darkIcons(color) ? 0xDE000000 : 0xFFFFFFFF;
            ArrayAdapter<String> adapter = new ArrayAdapter<String>(activity,
                    plain ? android.R.layout.simple_list_item_1
                            : android.R.layout.simple_list_item_single_choice, labels) {
                @Override public boolean areAllItemsEnabled() { return false; }
                @Override public boolean isEnabled(int position) { return !disabled[position]; }
                @Override public int getViewTypeCount() { return 2; }
                @Override public int getItemViewType(int position) { return indices[position] < 0 ? 1 : 0; }
                @Override public View getView(int position, View recycled, ViewGroup parent) {
                    TextView text;
                    if (indices[position] < 0) {
                        text = recycled instanceof TextView ? (TextView) recycled : new TextView(activity);
                        int pad = (int)(16 * activity.getResources().getDisplayMetrics().density);
                        text.setPadding(pad, pad, pad, pad / 2);
                        text.setTypeface(null, Typeface.BOLD);
                        if (examples) text.setTextSize(18);
                        text.setText(labels[position]);
                    } else {
                        text = (TextView) super.getView(position, recycled, parent);
                        if (examples) {
                            int indent = (int)(32 * activity.getResources().getDisplayMetrics().density);
                            text.setPaddingRelative(indent, text.getPaddingTop(),
                                    text.getPaddingEnd(), text.getPaddingBottom());
                        }
                    }
                    text.setTextColor(textColor);
                    text.setAlpha(disabled[position] && indices[position] >= 0 ? .5f : 1f);
                    return text;
                }
            };
            // Every dismissal must release the pending JavaScript prompt exactly once.
            boolean[] answered = {false};
            AlertDialog.Builder builder = new BackgroundDialogBuilder(activity);
            if (found) {
                TextView title = new TextView(activity);
                title.setText(R.string.found_dictionaries_title);
                title.setTextColor(textColor);
                title.setTextSize(20);
                int pad = (int)(20 * activity.getResources().getDisplayMetrics().density);
                title.setPadding(pad, pad, pad, pad / 2);
                builder.setCustomTitle(title);
            }
            android.content.DialogInterface.OnClickListener select = (d, which) -> {
                        if (disabled[which]) return;
                        answered[0] = true;
                        result.confirm(Integer.toString(indices[which]));
                        d.dismiss();
                    };
            if (rows.length() == 0) builder.setMessage(R.string.found_dictionaries_empty);
            else if (plain) builder.setAdapter(adapter, select);
            else builder.setSingleChoiceItems(adapter, checked, select);
            AlertDialog dialog = builder.setNegativeButton(android.R.string.cancel, (d, which) -> {})
                    .create();
            dialog.setOnDismissListener(d -> {
                if (!answered[0]) { answered[0] = true; result.cancel(); }
            });
            dialog.setCanceledOnTouchOutside(true);
            dialog.show();
            dialog.getWindow().setBackgroundDrawable(WindowBackground.dialogDrawable(activity, color));
            if (examples || "mode".equals(data.optString("kind"))) {
                android.util.DisplayMetrics metrics = activity.getResources().getDisplayMetrics();
                int width = Math.min((int)(260 * metrics.density), (int)(metrics.widthPixels * .9f));
                dialog.getWindow().setLayout(width, android.view.WindowManager.LayoutParams.WRAP_CONTENT);
            }
            if (dialog.getListView() != null) dialog.getListView().setBackgroundColor(android.graphics.Color.TRANSPARENT);
            TextView message = dialog.findViewById(android.R.id.message);
            if (message != null) message.setTextColor(textColor);
            dialog.getButton(AlertDialog.BUTTON_NEGATIVE).setTextColor(textColor);
        } catch (JSONException | IllegalArgumentException bad) {
            result.cancel();
        }
    }
}
