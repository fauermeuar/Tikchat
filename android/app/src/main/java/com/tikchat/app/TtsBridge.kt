package com.tikchat.app

import android.content.Context
import android.os.Bundle
import android.speech.tts.TextToSpeech
import android.speech.tts.Voice
import android.util.Log
import android.webkit.JavascriptInterface
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * TikChat v1.4.9 — bridge TTS native untuk WebView.
 *
 * WebView Android TIDAK punya window.speechSynthesis, jadi WebUI memakai
 * objek ini (window.AndroidTTS) bila tersedia:
 *
 *   getVoices() -> JSON [{id, name, lang}]
 *   speak(text, voiceId, volume)   // volume 0..1
 *   cancel()
 *   isSpeaking() -> boolean
 */
class TtsBridge(context: Context) {

    companion object {
        private const val TAG = "TikChatTts"
    }

    private var tts: TextToSpeech? = null
    private val ready = CountDownLatch(1)

    init {
        try {
            tts = TextToSpeech(context.applicationContext) { status ->
                if (status != TextToSpeech.SUCCESS) {
                    Log.w(TAG, "init TTS gagal: $status")
                }
                ready.countDown()
            }
        } catch (e: Exception) {
            Log.e(TAG, "init TTS error", e)
            ready.countDown()
        }
    }

    private fun awaitReady(): Boolean {
        return try {
            ready.await(8, TimeUnit.SECONDS)
        } catch (e: InterruptedException) {
            false
        }
    }

    private fun voices(): List<Voice> {
        if (!awaitReady()) return emptyList()
        return try {
            tts?.voices?.toList() ?: emptyList()
        } catch (e: Exception) {
            emptyList()
        }
    }

    @JavascriptInterface
    fun getVoices(): String {
        val arr = JSONArray()
        for (v in voices()) {
            try {
                val id = v.name ?: continue
                val o = JSONObject()
                o.put("id", id)
                o.put("name", v.name)
                o.put("lang", v.locale?.toLanguageTag() ?: "")
                arr.put(o)
            } catch (e: Exception) {
                // lewati voice yang rusak
            }
        }
        return arr.toString()
    }

    @JavascriptInterface
    fun speak(text: String, voiceId: String, volume: Float) {
        if (!awaitReady()) return
        val t = tts ?: return
        val msg = text.trim()
        if (msg.isEmpty()) return
        try {
            val v = voices().find { it.name == voiceId }
            if (v != null) {
                try {
                    t.voice = v
                } catch (e: Exception) {
                    Log.w(TAG, "setVoice gagal: ${e.message}")
                }
                try {
                    t.language = v.locale
                } catch (e: Exception) {
                }
            }
            val params = Bundle()
            params.putFloat(
                TextToSpeech.Engine.KEY_PARAM_VOLUME, volume.coerceIn(0f, 1f)
            )
            t.speak(msg, TextToSpeech.QUEUE_FLUSH, params, UUID.randomUUID().toString())
        } catch (e: Exception) {
            Log.w(TAG, "speak gagal: ${e.message}")
        }
    }

    @JavascriptInterface
    fun cancel() {
        try {
            tts?.stop()
        } catch (e: Exception) {
        }
    }

    @JavascriptInterface
    fun isSpeaking(): Boolean {
        return try {
            tts?.isSpeaking == true
        } catch (e: Exception) {
            false
        }
    }

    fun shutdown() {
        try {
            tts?.stop()
            tts?.shutdown()
        } catch (e: Exception) {
        }
        tts = null
    }
}
