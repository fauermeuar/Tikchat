package com.tikchat.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import java.io.File
import kotlin.concurrent.thread

/**
 * TikChat v1.4.9 — menjalankan backend Go sebagai foreground service.
 *
 * Biner Go (android/arm64) dibundel di assets/<abi>/tikchat oleh CI,
 * diekstrak ke filesDir lalu dijalankan:
 *   tikchat -daemon -datadir <filesDir>/data
 *
 * Di sini backend jalan sebagai UID aplikasi (bukan root), jadi
 * dropPrivileges() di Go tidak melakukan apa-apa — aman.
 */
class BackendService : Service() {

    companion object {
        private const val TAG = "TikChatBackend"
        private const val NOTIF_ID = 1001
        private const val CHANNEL_ID = "tikchat-backend"
    }

    private var proc: Process? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        startAsForeground()
        thread(name = "backend-run", isDaemon = true) { runBackend() }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Jangan restart otomatis bila sistem membunuh service.
        return START_NOT_STICKY
    }

    private fun startAsForeground() {
        val nm = getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            nm.createNotificationChannel(
                NotificationChannel(
                    CHANNEL_ID, "TikChat", NotificationManager.IMPORTANCE_LOW
                )
            )
        }
        val notif = Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("TikChat berjalan")
            .setContentText("Backend v1.4.9 aktif di 127.0.0.1:5658")
            .setSmallIcon(R.drawable.ic_notif)
            .setOngoing(true)
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(
                NOTIF_ID, notif, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            )
        } else {
            @Suppress("DEPRECATION")
            startForeground(NOTIF_ID, notif)
        }
    }

    private fun runBackend() {
        try {
            if (!Build.SUPPORTED_ABIS.contains("arm64-v8a")) {
                Log.e(TAG, "perangkat bukan arm64 — backend tak bisa jalan")
                stopSelf()
                return
            }
            val bin = File(filesDir, "tikchat")
            extractBinary(bin)
            val dataDir = File(filesDir, "data").apply { mkdirs() }

            val pb = ProcessBuilder(
                bin.absolutePath, "-daemon", "-datadir", dataDir.absolutePath
            )
            pb.redirectErrorStream(true)
            // Lingkungan minimal agar DNS/eksekusi stabil.
            pb.environment()["PATH"] = "/system/bin:/vendor/bin"
            proc = pb.start()
            Log.i(TAG, "backend dinyalakan: ${bin.absolutePath}")

            // Pompa log backend ke logcat (diagnostik).
            thread(name = "backend-log", isDaemon = true) {
                try {
                    proc?.inputStream?.bufferedReader()?.forEachLine { line ->
                        Log.i(TAG, "backend: $line")
                    }
                } catch (e: Exception) {
                    Log.w(TAG, "pompa log berhenti: ${e.message}")
                }
            }

            val code = proc?.waitFor() ?: -1
            Log.i(TAG, "backend berhenti (exit=$code)")
        } catch (e: Exception) {
            Log.e(TAG, "backend gagal dinyalakan", e)
        }
    }

    /** Salin biner dari assets ke filesDir lalu jadikan executable. */
    private fun extractBinary(bin: File) {
        assets.open("arm64-v8a/tikchat").use { inp ->
            bin.outputStream().use { out -> inp.copyTo(out) }
        }
        if (!bin.setExecutable(true, true)) {
            Log.w(TAG, "setExecutable gagal untuk ${bin.absolutePath}")
        }
        Log.i(TAG, "biner diekstrak: ${bin.length()} bytes")
    }

    override fun onDestroy() {
        try {
            proc?.destroy()
            // Beri waktu berhenti rapi, lalu paksa bila masih hidup.
            thread {
                Thread.sleep(1500)
                try { proc?.destroyForcibly() } catch (e: Exception) {}
            }
        } catch (e: Exception) {
            Log.w(TAG, "gagal menghentikan backend: ${e.message}")
        }
        super.onDestroy()
    }
}
