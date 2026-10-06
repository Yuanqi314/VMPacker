package com.vmpacker.app

import android.content.Context
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.snapshots.SnapshotStateList
import com.vmpacker.vmpmobile.Logger
import java.io.File
import java.io.FileNotFoundException

// ============================================================================
// Storage Access Framework <-> filesystem bridge.
//
// The Go engine (via gomobile) works on real filesystem paths, while SAF hands
// out content:// Uris. These helpers copy a picked input Uri into app-private
// cacheDir (no permission needed) and copy the produced output File back out to
// a user-chosen destination Uri.
// ============================================================================

/** Copy a user-picked content:// Uri into cacheDir and return the concrete File. */
fun copyUriToCache(context: Context, src: Uri, fileName: String): File {
    val dest = File(context.cacheDir, fileName)
    context.contentResolver.openInputStream(src)?.use { input ->
        dest.outputStream().use { output -> input.copyTo(output) }
    } ?: throw FileNotFoundException("无法打开输入文件: $src")
    return dest
}

/** Copy a Go-produced result File back out to the destination content:// Uri. */
fun copyFileToUri(context: Context, result: File, dest: Uri) {
    context.contentResolver.openOutputStream(dest)?.use { output ->
        result.inputStream().use { input -> input.copyTo(output) }
    } ?: throw FileNotFoundException("无法写入输出文件: $dest")
}

/** Resolve a display name for a content:// Uri, falling back when unavailable. */
fun displayName(context: Context, uri: Uri, fallback: String): String {
    return try {
        context.contentResolver.query(uri, null, null, null, null)?.use { c ->
            val idx = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            if (idx >= 0 && c.moveToFirst()) c.getString(idx) ?: fallback else fallback
        } ?: fallback
    } catch (_: Exception) {
        fallback
    }
}

/**
 * Logger implementation handed to the Go engine. Go invokes [log] from a
 * background JNI thread, so each line is posted to the main thread before
 * mutating the Compose-observable [lines] list.
 */
class ComposeLogger(
    val lines: SnapshotStateList<String> = mutableStateListOf(),
) : Logger {
    private val main = Handler(Looper.getMainLooper())

    override fun log(line: String) {
        main.post { lines.add(line) }
    }
}
