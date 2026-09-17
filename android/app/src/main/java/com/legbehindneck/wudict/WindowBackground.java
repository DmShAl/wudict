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
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/** Reads the same store as the CSS Files panel, without waiting for the server. */
final class WindowBackground {
    private static Bitmap cached;
    private static String cachedKey = "";

    static File directory(Context c) {
        return new File(AppDirs.home(c), ".wudict/style/assets");
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
        String name = ShellPrefs.of(c).getString("background_image", "");
        if (!name.matches("[A-Za-z0-9][A-Za-z0-9._-]{0,63}")) return null;
        File file = new File(directory(c), name);
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
