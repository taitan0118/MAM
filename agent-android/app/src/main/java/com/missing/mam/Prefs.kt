package com.missing.mam

import android.content.Context
import java.util.UUID

// Lưu cấu hình agent: địa chỉ server, token sau khi đăng ký, và một mã máy ổn định (thay cho MAC —
// Android không cho đọc MAC thật nữa). Dùng SharedPreferences, không cần thư viện ngoài.
class Prefs(ctx: Context) {
    private val sp = ctx.getSharedPreferences("mam", Context.MODE_PRIVATE)

    var server: String
        get() = sp.getString("server", "") ?: ""
        set(v) = sp.edit().putString("server", v).apply()

    var enrollKey: String
        get() = sp.getString("key", "") ?: ""
        set(v) = sp.edit().putString("key", v).apply()

    var token: String
        get() = sp.getString("token", "") ?: ""
        set(v) = sp.edit().putString("token", v).apply()

    var id: String
        get() = sp.getString("id", "") ?: ""
        set(v) = sp.edit().putString("id", v).apply()

    // deviceId ổn định, dùng làm "mac" để server nhận ra cùng một máy khi cài lại.
    val deviceId: String
        get() {
            var d = sp.getString("devid", null)
            if (d == null) {
                d = "ANDROID-" + UUID.randomUUID().toString().substring(0, 12).uppercase()
                sp.edit().putString("devid", d).apply()
            }
            return d
        }
}
