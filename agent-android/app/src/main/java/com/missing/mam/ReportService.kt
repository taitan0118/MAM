package com.missing.mam

import android.app.*
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.*
import android.app.ActivityManager
import org.json.JSONArray
import org.json.JSONObject
import kotlin.concurrent.thread

// Dịch vụ nền hiện thông báo thường trực (người dùng luôn biết máy đang được quản lý — minh bạch,
// đúng yêu cầu của Android và của sản phẩm), báo tình trạng máy về server mỗi 5 giây, và nhận lệnh
// nhắn tin. KHÔNG xem màn hình, KHÔNG điều khiển máy từ xa.
class ReportService : Service() {
    @Volatile private var running = false
    private val chan = "mam"

    override fun onBind(i: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(1, notif("Máy đang được quản lý bởi MAM", "Đang kết nối…"))
        if (!running) {
            running = true
            thread { loop() }
        }
        return START_STICKY
    }

    private fun loop() {
        val p = Prefs(this)
        while (running) {
            val server = p.server
            if (server.isEmpty()) { sleep(5000); continue }
            if (p.token.isEmpty()) {
                val r = Api.enroll(server, p.enrollKey, Build.MODEL ?: "Android", osName(), p.deviceId, hw())
                if (r != null && r.optInt("_code") == 200) {
                    p.id = r.optString("id"); p.token = r.optString("token")
                } else { sleep(10000); continue }
            }
            val r = Api.report(server, p.token, metrics())
            when {
                r == null -> update("Mất kết nối server")
                r.optInt("_code") == 401 -> { p.token = ""; continue }   // server cấp lại token
                r.optInt("_code") == 403 -> { sleep(60000); continue }    // máy đã bị gỡ khỏi hệ thống
                r.optInt("_code") == 200 -> {
                    update("Đã kết nối • cập nhật lúc " + now())
                    handleCommands(server, p.token, Api.commandsOf(r))
                }
            }
            sleep(5000)
        }
    }

    private fun handleCommands(server: String, token: String, cmds: JSONArray) {
        for (i in 0 until cmds.length()) {
            val c = cmds.optJSONObject(i) ?: continue
            val id = c.optString("id")
            when (c.optString("type")) {
                "message" -> {
                    val args = c.optJSONObject("args") ?: JSONObject()
                    val text = args.optString("text")
                    (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
                        .notify(2, notif("Tin nhắn từ quản trị viên", text))
                    Api.postResult(server, token, id, true, "Đã hiển thị tin trên máy")
                }
                else -> Api.postResult(server, token, id, false, "Lệnh này không hỗ trợ trên Android")
            }
        }
    }

    // ---- thu thập số liệu ----
    private fun metrics(): JSONObject {
        val mi = ActivityManager.MemoryInfo()
        (getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager).getMemoryInfo(mi)
        val ram = if (mi.totalMem > 0) 100.0 * (mi.totalMem - mi.availMem) / mi.totalMem else 0.0

        val st = StatFs(Environment.getDataDirectory().path)
        val totalGB = st.blockCountLong * st.blockSizeLong / 1e9
        val freeGB = st.availableBlocksLong * st.blockSizeLong / 1e9

        val bat = registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val temp = bat?.getIntExtra("temperature", -1) ?: -1   // phần mười °C
        val lvl = bat?.getIntExtra("level", -1) ?: -1
        val scale = bat?.getIntExtra("scale", 100) ?: 100
        val pct = if (lvl >= 0) 100 * lvl / scale else -1

        val disks = JSONArray().put(JSONObject()
            .put("name", "Bộ nhớ").put("totalGB", totalGB).put("freeGB", freeGB))

        return JSONObject()
            .put("cpu", 0)                       // Android không cho đọc CPU toàn hệ thống
            .put("ram", ram)
            .put("temp", if (temp > 0) temp / 10.0 else JSONObject.NULL)
            .put("uptime", SystemClock.elapsedRealtime() / 1000)
            .put("disks", disks)
            .put("user", if (pct >= 0) "Pin $pct%" else "")
            .put("ip", "")
            .put("ver", "1.1.0")
            .put("arch", Build.SUPPORTED_ABIS.firstOrNull() ?: "android")
            .put("hw", hw().put("battery", pct))
    }

    private fun hw() = JSONObject()
        .put("cpu", Build.HARDWARE ?: "")
        .put("os", osName())
        .put("model", (Build.MANUFACTURER ?: "") + " " + (Build.MODEL ?: ""))
        .put("board", Build.DEVICE ?: "")
        .put("serial", Prefs(this).deviceId)

    private fun osName() = "Android " + (Build.VERSION.RELEASE ?: "")

    // ---- thông báo thường trực ----
    private fun notif(title: String, text: String): Notification {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= 26) {
            nm.createNotificationChannel(NotificationChannel(chan, "MAM", NotificationManager.IMPORTANCE_LOW))
        }
        val pi = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE)
        return Notification.Builder(this, chan)
            .setContentTitle(title).setContentText(text)
            .setSmallIcon(android.R.drawable.ic_menu_compass)
            .setContentIntent(pi).setOngoing(true).build()
    }

    private fun update(text: String) {
        (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
            .notify(1, notif("Máy đang được quản lý bởi MAM", text))
    }

    private fun now(): String {
        val c = java.util.Calendar.getInstance()
        return "%02d:%02d".format(c.get(java.util.Calendar.HOUR_OF_DAY), c.get(java.util.Calendar.MINUTE))
    }

    private fun sleep(ms: Long) = try { Thread.sleep(ms) } catch (e: InterruptedException) {}

    override fun onDestroy() { running = false; super.onDestroy() }
}
