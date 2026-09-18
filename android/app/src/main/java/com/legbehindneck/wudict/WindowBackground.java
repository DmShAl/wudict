package com.legbehindneck.wudict;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.graphics.Canvas;
import android.graphics.ColorFilter;
import android.graphics.Paint;
import android.graphics.PixelFormat;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.ColorDrawable;
import java.io.File;
import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/** Reads the same store as the CSS Files panel, without waiting for the server. */
final class WindowBackground {
    private static Bitmap cached;
    private static String cachedKey = "";

    static synchronized File directory(Context c) {
        File dir = new File(AppDirs.home(c), ".wudict/style/assets");
        // Publish complete files only, and never replace an imported user file.
        for (String name : new String[]{"paper_01.jpg", "paper_02.jpg"}) {
            File target = new File(dir, name);
            if (target.exists()) continue;
            File temporary = null;
            try {
                if (!dir.isDirectory() && !dir.mkdirs()) continue;
                temporary = File.createTempFile(".paper-", ".tmp", dir);
                try (InputStream input = c.getAssets().open("backgrounds/" + name)) {
                    Files.copy(input, temporary.toPath(), StandardCopyOption.REPLACE_EXISTING);
                }
                // No REPLACE_EXISTING: another writer's file always wins.
                Files.move(temporary.toPath(), target.toPath());
            } catch (IOException | SecurityException ignored) {
                // Storage may be unavailable; ordinary backgrounds still work.
            } finally {
                if (temporary != null) {
                    try { temporary.delete(); } catch (SecurityException ignored) {}
                }
            }
        }
        return dir;
    }

    static List<String> images(Context c) {
        List<String> result = new ArrayList<>();
        File[] files = directory(c).listFiles();
        if (files == null) return result;
        Arrays.sort(files, (a, b) -> a.getName().compareToIgnoreCase(b.getName()));
        for (File file : files) {
            if (!file.isFile() || !file.getName().matches("[A-Za-z0-9][A-Za-z0-9._-]{0,63}")) continue;
            BitmapFactory.Options bounds = new BitmapFactory.Options();
            bounds.inJustDecodeBounds = true;
            BitmapFactory.decodeFile(file.getPath(), bounds);
            if (bounds.outWidth > 0 && bounds.outHeight > 0) result.add(file.getName());
        }
        return result;
    }

    private static synchronized Bitmap bitmap(Context c) {
        File dir = directory(c);
        String name = ShellPrefs.of(c).getString("background_image", "");
        if (!name.matches("[A-Za-z0-9][A-Za-z0-9._-]{0,63}")) return null;
        File file = new File(dir, name);
        if (!file.isFile()) { cached = null; cachedKey = ""; return null; }
        String key = file.getPath() + ":" + file.lastModified() + ":" + file.length();
        if (key.equals(cachedKey)) return cached;
        BitmapFactory.Options options = new BitmapFactory.Options();
        options.inJustDecodeBounds = true;
        BitmapFactory.decodeFile(file.getPath(), options);
        options.inSampleSize = 1;
        // Bound decoded memory even for a very large compressed wallpaper.
        while ((long)(options.outWidth / options.inSampleSize)
                * (options.outHeight / options.inSampleSize) > 4_000_000L) options.inSampleSize *= 2;
        options.inJustDecodeBounds = false;
        try { cached = BitmapFactory.decodeFile(file.getPath(), options); }
        catch (OutOfMemoryError bad) { cached = null; }
        cachedKey = key;
        return cached;
    }

    static boolean active(Context c) { return bitmap(c) != null; }

    /** A slightly lighter surface, with a rounded outline separating dialogs from the page. */
    static Drawable dialogDrawable(Context c, int color) {
        Drawable base = drawable(c, color);
        float density = c.getResources().getDisplayMetrics().density;
        return new Drawable() {
            private final Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);
            @Override public void draw(Canvas canvas) {
                android.graphics.RectF area = new android.graphics.RectF(getBounds());
                android.graphics.Path clip = new android.graphics.Path();
                clip.addRoundRect(area, 12 * density, 12 * density, android.graphics.Path.Direction.CW);
                int saved = canvas.save();
                canvas.clipPath(clip);
                base.setBounds(getBounds());
                base.draw(canvas);
                canvas.drawColor(0x24FFFFFF);
                canvas.restoreToCount(saved);
                area.inset(density / 2, density / 2);
                paint.setStyle(Paint.Style.STROKE);
                paint.setStrokeWidth(density);
                paint.setColor(ShellPrefs.darkIcons(color) ? 0x66594B38 : 0x669F9586);
                canvas.drawRoundRect(area, 12 * density, 12 * density, paint);
            }
            @Override public void setAlpha(int alpha) { base.setAlpha(alpha); }
            @Override public void setColorFilter(ColorFilter filter) { base.setColorFilter(filter); }
            @Override public int getOpacity() { return PixelFormat.TRANSLUCENT; }
        };
    }

    /** Keep the page image inside the current content insets, not over the margins. */
    static Drawable withMargins(Context c, android.view.View view, int edgeColor) {
        Drawable page = drawable(c, ShellPrefs.pageBg(c));
        return new Drawable() {
            @Override public void draw(Canvas canvas) {
                int saved = canvas.save();
                canvas.clipRect(getBounds());
                canvas.drawColor(edgeColor);
                int left = getBounds().left + view.getPaddingLeft();
                int top = getBounds().top + view.getPaddingTop();
                int right = getBounds().right - view.getPaddingRight();
                int bottom = getBounds().bottom - view.getPaddingBottom();
                if (right > left && bottom > top) {
                    canvas.clipRect(left, top, right, bottom);
                    page.setBounds(left, top, right, bottom);
                    page.draw(canvas);
                }
                canvas.restoreToCount(saved);
            }
            @Override public void setAlpha(int alpha) { page.setAlpha(alpha); }
            @Override public void setColorFilter(ColorFilter filter) { page.setColorFilter(filter); }
            @Override public int getOpacity() { return PixelFormat.OPAQUE; }
        };
    }

    static Drawable drawable(Context c, int color) {
        Bitmap image = bitmap(c);
        if (image == null) return new ColorDrawable(color);
        return new Drawable() {
            private final Paint paint = new Paint(Paint.FILTER_BITMAP_FLAG);
            @Override public void draw(Canvas canvas) {
                canvas.drawColor(color);
                // Stretch each axis independently: show the whole image,
                // exactly filling this window even when its aspect ratio differs.
                canvas.drawBitmap(image, null, getBounds(), paint);
            }
            @Override public void setAlpha(int alpha) { paint.setAlpha(alpha); }
            @Override public void setColorFilter(ColorFilter filter) { paint.setColorFilter(filter); }
            @Override public int getOpacity() { return PixelFormat.OPAQUE; }
        };
    }
}
