package com.missing.mam

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

// Tự chạy lại sau khi khởi động máy, nếu đã cấu hình server.
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, i: Intent) {
        if (i.action == Intent.ACTION_BOOT_COMPLETED && Prefs(ctx).server.isNotEmpty()) {
            ctx.startForegroundService(Intent(ctx, ReportService::class.java))
        }
    }
}
