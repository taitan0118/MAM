package com.missing.mam

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.view.Gravity
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.widget.*
import android.app.Activity

// Màn hình duy nhất: nhập địa chỉ server + mã đăng ký, bật/tắt quản lý. Hiện rõ máy này đang được
// quản lý và báo những gì — minh bạch với người dùng, không có chế độ ẩn.
class MainActivity : Activity() {
    override fun onCreate(s: Bundle?) {
        super.onCreate(s)
        val p = Prefs(this)
        val pad = (16 * resources.displayMetrics.density).toInt()

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(pad, pad, pad, pad)
        }
        fun label(t: String) = TextView(this).apply { text = t; textSize = 13f; setPadding(0, pad / 2, 0, 4) }

        root.addView(TextView(this).apply {
            text = "MAM Agent"; textSize = 22f; gravity = Gravity.CENTER; setPadding(0, 0, 0, pad / 2)
        })
        root.addView(TextView(this).apply {
            text = "Máy này được quản lý bởi phần mềm MAM của cơ quan. Khi bật, máy sẽ báo " +
                "model, phiên bản Android, dung lượng bộ nhớ, mức pin và trạng thái online về máy chủ quản trị. " +
                "Phần mềm KHÔNG xem màn hình và KHÔNG điều khiển máy của bạn."
            textSize = 12.5f; setPadding(0, 0, 0, pad)
        })

        root.addView(label("Địa chỉ máy chủ (ví dụ 192.168.1.10:7070)"))
        val srv = EditText(this).apply { setText(p.server); hint = "192.168.1.10:7070" }
        root.addView(srv)

        root.addView(label("Mã đăng ký (nếu cơ quan có đặt)"))
        val key = EditText(this).apply { setText(p.enrollKey) }
        root.addView(key)

        val status = TextView(this).apply { textSize = 12.5f; setPadding(0, pad, 0, pad) }
        fun refresh() { status.text = if (p.token.isNotEmpty()) "Trạng thái: đã đăng ký (mã máy ${p.id})" else "Trạng thái: chưa đăng ký" }
        refresh()

        root.addView(Button(this).apply {
            text = "Bật quản lý"
            setOnClickListener {
                var v = srv.text.toString().trim()
                if (v.isEmpty()) { toast("Nhập địa chỉ máy chủ"); return@setOnClickListener }
                if (!v.startsWith("http")) v = "http://$v"
                p.server = v; p.enrollKey = key.text.toString().trim()
                askNotif()
                startService(Intent(this@MainActivity, ReportService::class.java))
                toast("Đã bật. Xem thông báo thường trực trên máy."); refresh()
            }
        })
        root.addView(Button(this).apply {
            text = "Tắt quản lý"
            setOnClickListener {
                stopService(Intent(this@MainActivity, ReportService::class.java))
                toast("Đã tắt quản lý trên máy này.")
            }
        })
        root.addView(status)

        val scroll = ScrollView(this).apply { addView(root); layoutParams = FrameLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT) }
        setContentView(scroll)

        if (p.server.isNotEmpty()) startService(Intent(this, ReportService::class.java))
    }

    private fun askNotif() {
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
        }
    }

    private fun toast(t: String) = Toast.makeText(this, t, Toast.LENGTH_SHORT).show()
}
