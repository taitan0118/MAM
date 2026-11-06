package com.missing.mam

import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

// Gọi API server bằng HttpURLConnection (có sẵn trong Android, khỏi thư viện ngoài).
// Cùng giao thức với agent Windows/Linux/macOS: enroll lấy token, rồi report định kỳ.
object Api {
    private fun post(server: String, path: String, token: String, body: JSONObject): JSONObject? {
        return try {
            val c = (URL(server.trimEnd('/') + path).openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                connectTimeout = 10000
                readTimeout = 20000
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
                if (token.isNotEmpty()) setRequestProperty("Authorization", "Bearer $token")
            }
            c.outputStream.use { it.write(body.toString().toByteArray()) }
            val code = c.responseCode
            val txt = (if (code in 200..299) c.inputStream else c.errorStream)
                ?.bufferedReader()?.use { it.readText() } ?: ""
            JSONObject(if (txt.isBlank()) "{}" else txt).put("_code", code)
        } catch (e: Exception) {
            null
        }
    }

    // enroll: đăng ký máy, trả token. name = tên máy, hw = thông tin phần cứng.
    fun enroll(server: String, key: String, name: String, os: String, devId: String, hw: JSONObject): JSONObject? {
        val b = JSONObject()
            .put("name", name).put("os", os).put("mac", devId)
            .put("ip", "").put("key", key).put("hw", hw)
        return post(server, "/api/agent/enroll", "", b)
    }

    // report: gửi tình trạng, nhận lệnh (nếu có) trong "commands".
    fun report(server: String, token: String, body: JSONObject): JSONObject? {
        return post(server, "/api/agent/report", token, body)
    }

    fun commandsOf(resp: JSONObject): JSONArray = resp.optJSONArray("commands") ?: JSONArray()

    // postResult: báo server kết quả một lệnh để Console hiện "đã xong".
    fun postResult(server: String, token: String, id: String, ok: Boolean, output: String) {
        post(server, "/api/agent/result", token, JSONObject().put("id", id).put("ok", ok).put("output", output))
    }
}
