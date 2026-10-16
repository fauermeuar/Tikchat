package com.tikchat.app

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.view.View
import android.webkit.PermissionRequest
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread

/**
 * TikChat v1.4.9 — Activity utama (tanpa dependency AndroidX).
 *
 * - Menyalakan [BackendService] (backend Go di 127.0.0.1:5658).
 * - Menunggu backend siap, lalu memuat WebUI di WebView.
 * - WebView: JS + DOM storage + izin mic (tab Audio) + file chooser (MP3).
 * - Bridge TTS native: window.AndroidTTS (WebView tak punya speechSynthesis).
 */
class MainActivity : Activity() {

    companion object {
        private const val BACKEND_URL = "http://127.0.0.1:5658/tikchat/"
        private const val STATUS_URL = "http://127.0.0.1:5658/api/status"
        private const val PERM_REQ = 41
        private const val FILE_REQ = 42
    }

    private lateinit var web: WebView
    private lateinit var loading: View
    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private var ttsBridge: TtsBridge? = null

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        web = findViewById(R.id.web)
        loading = findViewById(R.id.loading)

        requestRuntimePermissions()

        // Backend Go sebagai foreground service (tetap jalan saat layar mati).
        val svc = Intent(this, BackendService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(svc)
        } else {
            startService(svc)
        }

        ttsBridge = TtsBridge(this)
        web.addJavascriptInterface(ttsBridge!!, "AndroidTTS")

        val ws = web.settings
        ws.javaScriptEnabled = true
        ws.domStorageEnabled = true
        ws.mediaPlaybackRequiresUserGesture = false
        ws.allowFileAccess = false
        ws.allowContentAccess = true

        web.webViewClient = WebViewClient()
        web.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                runOnUiThread {
                    if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) ==
                        PackageManager.PERMISSION_GRANTED
                    ) {
                        request.grant(request.resources)
                    } else {
                        request.deny()
                    }
                }
            }

            override fun onShowFileChooser(
                view: WebView,
                callback: ValueCallback<Array<Uri>>,
                params: FileChooserParams
            ): Boolean {
                fileCallback?.onReceiveValue(null)
                fileCallback = callback
                val intent = Intent(Intent.ACTION_GET_CONTENT).apply {
                    addCategory(Intent.CATEGORY_OPENABLE)
                    type = "audio/*"
                    if (params.mode == FileChooserParams.MODE_OPEN_MULTIPLE) {
                        putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
                    }
                }
                return try {
                    @Suppress("DEPRECATION")
                    startActivityForResult(Intent.createChooser(intent, "Pilih audio"), FILE_REQ)
                    true
                } catch (e: Exception) {
                    fileCallback = null
                    callback.onReceiveValue(null)
                    false
                }
            }
        }

        // Tunggu backend siap sebelum memuat WebUI.
        thread(name = "backend-wait", isDaemon = true) {
            if (waitForBackend(45_000)) {
                runOnUiThread {
                    loading.visibility = View.GONE
                    web.loadUrl(BACKEND_URL)
                }
            } else {
                runOnUiThread {
                    Toast.makeText(
                        this, "Backend tak bisa dinyalakan — coba buka ulang aplikasi",
                        Toast.LENGTH_LONG
                    ).show()
                }
            }
        }
    }

    @Deprecated("pakai API framework agar tanpa AndroidX")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != FILE_REQ) return
        val cb = fileCallback
        fileCallback = null
        if (cb == null) return
        if (resultCode == RESULT_OK) {
            val uris = mutableListOf<Uri>()
            data?.clipData?.let { cd ->
                for (i in 0 until cd.itemCount) uris.add(cd.getItemAt(i).uri)
            }
            data?.data?.let { uris.add(it) }
            cb.onReceiveValue(uris.toTypedArray())
        } else {
            cb.onReceiveValue(null)
        }
    }

    private fun requestRuntimePermissions() {
        val need = mutableListOf<String>()
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) !=
            PackageManager.PERMISSION_GRANTED
        ) need.add(Manifest.permission.RECORD_AUDIO)
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) need.add(Manifest.permission.POST_NOTIFICATIONS)
        if (need.isNotEmpty()) requestPermissions(need.toTypedArray(), PERM_REQ)
    }

    /** Poll /api/status sampai 200 atau timeout. */
    private fun waitForBackend(timeoutMs: Long): Boolean {
        val end = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < end) {
            try {
                val c = URL(STATUS_URL).openConnection() as HttpURLConnection
                c.connectTimeout = 1000
                c.readTimeout = 1000
                c.requestMethod = "GET"
                if (c.responseCode == 200) {
                    c.disconnect()
                    return true
                }
                c.disconnect()
            } catch (e: Exception) {
                // backend belum nyala — coba lagi
            }
            Thread.sleep(500)
        }
        return false
    }

    @Deprecated("pakai API framework agar tanpa AndroidX")
    override fun onBackPressed() {
        if (::web.isInitialized && web.canGoBack()) web.goBack()
        else super.onBackPressed()
    }

    override fun onDestroy() {
        ttsBridge?.shutdown()
        super.onDestroy()
    }
}
