package com.legbehindneck.wudict;

import android.app.Activity;
import android.app.AlertDialog;
import android.graphics.Typeface;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.JsPromptResult;
import android.widget.ArrayAdapter;
import android.widget.TextView;
import android.widget.Spinner;
import android.widget.ListView;
import android.widget.LinearLayout;
import android.widget.AdapterView;
import android.webkit.WebView;
import java.util.WeakHashMap;
import org.json.JSONArray;
import org.json.JSONObject;
import org.json.JSONException;

/** A themed replacement for WebView's dictionary select popup. */
final class DictionaryPicker {
    private static final WeakHashMap<WebView, Live> live = new WeakHashMap<>();

    // The opening prompt is answered immediately. JavaScript may then search and
    // stream new rows while this native window remains open.
    static void showLive(Activity activity, WebView web, String payload, JsPromptResult result) {
        if (activity.isFinishing() || activity.isDestroyed()) { result.cancel(); return; }
        try {
            Live previous = live.remove(web);
            if (previous != null) previous.dialog.dismiss();
            Live picker = new Live(activity, web, payload);
            live.put(web, picker);
            picker.dialog.show();
            picker.dialog.getWindow().setBackgroundDrawable(
                    WindowBackground.dialogDrawable(activity, ShellPrefs.pageBg(activity)));
            picker.dialog.getButton(AlertDialog.BUTTON_NEGATIVE).setTextColor(
                    ShellPrefs.darkIcons(ShellPrefs.pageBg(activity)) ? 0xDE000000 : 0xFFFFFFFF);
            result.confirm("open");
        } catch (JSONException bad) { result.cancel(); }
    }

    static void updateLive(WebView web, String payload) {
        Live picker = live.get(web);
        if (picker == null) return;
        try { picker.update(new JSONObject(payload)); }
        catch (JSONException ignored) { }
    }

    private static final class Live {
        final WebView web;
        final AlertDialog dialog;
        final Spinner groups;
        final ListView list;
        final ArrayAdapter<String> groupAdapter, rowAdapter;
        JSONArray rows = new JSONArray();
        JSONArray groupValues = new JSONArray();
        boolean updating;
        final boolean found;
        String groupId = "all";

        Live(Activity activity, WebView web, String payload) throws JSONException {
            this.web = web;
            found = new JSONObject(payload).optBoolean("found");
            int color = ShellPrefs.pageBg(activity);
            int textColor = ShellPrefs.darkIcons(color) ? 0xDE000000 : 0xFFFFFFFF;
            int pad = (int)(12 * activity.getResources().getDisplayMetrics().density);
            LinearLayout body = new LinearLayout(activity);
            body.setOrientation(LinearLayout.VERTICAL);
            body.setPadding(pad, pad, pad, pad);
            groups = new Spinner(activity);
            groups.setPopupBackgroundDrawable(WindowBackground.dialogDrawable(activity, color));
            groupAdapter = new ArrayAdapter<String>(activity, android.R.layout.simple_spinner_item) {
                @Override public View getView(int position, View recycled, ViewGroup parent) {
                    TextView text = (TextView) super.getView(position, recycled, parent);
                    text.setTextColor(textColor);
                    return text;
                }
                @Override public View getDropDownView(int position, View recycled, ViewGroup parent) {
                    TextView text = (TextView) super.getDropDownView(position, recycled, parent);
                    text.setTextColor(textColor);
                    return text;
                }
            };
            groupAdapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
            groups.setAdapter(groupAdapter);
            body.addView(groups);
            View separator = new View(activity);
            separator.setBackgroundColor((textColor & 0x00FFFFFF) | 0x33000000);
            LinearLayout.LayoutParams separatorLayout = new LinearLayout.LayoutParams(
                    ViewGroup.LayoutParams.MATCH_PARENT,
                    Math.max(1, (int) activity.getResources().getDisplayMetrics().density));
            separatorLayout.topMargin = pad;
            separatorLayout.bottomMargin = pad / 2;
            body.addView(separator, separatorLayout);
            list = new ListView(activity);
            rowAdapter = new ArrayAdapter<String>(activity, found
                    ? android.R.layout.simple_list_item_1
                    : android.R.layout.simple_list_item_single_choice) {
                @Override public View getView(int position, View recycled, ViewGroup parent) {
                    TextView text = (TextView) super.getView(position, recycled, parent);
                    text.setTextColor(textColor);
                    return text;
                }
            };
            list.setAdapter(rowAdapter);
            if (!found) list.setChoiceMode(ListView.CHOICE_MODE_SINGLE);
            list.setMinimumHeight((int)(220 * activity.getResources().getDisplayMetrics().density));
            body.addView(list, new LinearLayout.LayoutParams(
                    ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));
            dialog = new BackgroundDialogBuilder(activity)
                    .setView(body)
                    .setNegativeButton(android.R.string.cancel, (d, which) -> {})
                    .create();
            dialog.setOnDismissListener(d -> {
                if (live.get(web) == this) {
                    live.remove(web);
                    web.evaluateJavascript("window.wudictPickerClosed&&window.wudictPickerClosed()", null);
                }
            });
            groups.setOnItemSelectedListener(new AdapterView.OnItemSelectedListener() {
                @Override public void onNothingSelected(AdapterView<?> parent) { }
                @Override public void onItemSelected(AdapterView<?> parent, View view, int position, long id) {
                    if (updating) return;
                    try {
                        if (position >= groupValues.length()) return;
                        String chosen = groupValues.getJSONObject(position).getString("id");
                        if (!chosen.equals(groupId)) {
                            groupId = chosen;
                            web.evaluateJavascript("window.wudictPickerGroupChanged("+
                                    JSONObject.quote(chosen)+")", null);
                        }
                    } catch (JSONException ignored) { }
                }
            });
            list.setOnItemClickListener((parent, view, position, id) -> {
                try {
                    if (position >= rows.length()) return;
                    String value = rows.getJSONObject(position).optString("id");
                    if (value.isEmpty()) return;
                    web.evaluateJavascript("window.wudictPickerDictionarySelected("+
                            JSONObject.quote(value)+")", null);
                    dialog.dismiss();
                } catch (JSONException ignored) { }
            });
            update(new JSONObject(payload));
        }

        void update(JSONObject data) throws JSONException {
            updating = true;
            JSONArray values = data.getJSONArray("groups");
            groupValues = values;
            groupId = data.optString("group", "all");
            groupAdapter.clear();
            int selected = 0;
            for (int i = 0; i < values.length(); i++) {
                JSONObject value = values.getJSONObject(i);
                groupAdapter.add(value.getString("name"));
                if (groupId.equals(value.getString("id"))) selected = i;
            }
            groupAdapter.notifyDataSetChanged();
            groups.setSelection(selected);
            rows = data.getJSONArray("rows");
            int first = list.getFirstVisiblePosition();
            View top = list.getChildAt(0);
            int offset = top == null ? 0 : top.getTop();
            rowAdapter.clear();
            int checked = -1;
            for (int i = 0; i < rows.length(); i++) {
                JSONObject row = rows.getJSONObject(i);
                rowAdapter.add(row.getString("label"));
                if (row.optString("id").equals(data.optString("selected"))) checked = i;
            }
            if (rows.length() == 0) rowAdapter.add(data.optString("empty", "No dictionaries"));
            rowAdapter.notifyDataSetChanged();
            if (!found) {
                list.clearChoices();
                if (checked >= 0) list.setItemChecked(checked, true);
            }
            if (top != null) list.setSelectionFromTop(Math.min(first, rowAdapter.getCount() - 1), offset);
            updating = false;
        }
    }

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
