# TikChat v1.4.9 — Android APK

Pantau + bacakan komentar TikTok LIVE tanpa membuka video (hemat kuota),
sebagai **aplikasi Android** (bukan lagi modul Magisk).

## Fitur v1.4.9

- **Pantau LIVE**: cukup username TikTok — komentar real-time tanpa unduh video.
- **Username tersimpan**: chip username, 1 tap untuk pantau ulang (tersimpan permanen).
- **Bacakan komentar (TTS)**: pakai suara TTS HP, pilih suara (Indonesia diutamakan),
  anti-menumpuk saat live ramai.
- **Tab Audio** (baru):
  - 🎵 **Musik**: putar MP3 dari HP (tombol MP3/Folder), tersimpan permanen di
    aplikasi & dimuat otomatis, prev/play/next.
  - 🎤 **Mikrofon**: suara mic langsung ke speaker HP (awas feedback).
  - 🎧 **Volume**: slider Suara komentar / Musik / Mikrofon.
  - 📋 **Log**: panel log aktivitas aplikasi.
- Status koneksi, durasi pantau, statistik penonton & komentar, cari komentar.

## Struktur repo

```
tikchat-android/
├── backend/                  # Backend Go (pantau TikTok LIVE + serve WebUI)
│   ├── *.go                  # tiktok.go, server.go, saved.go, dns.go, main.go
│   └── webui/                # WebUI (index.html, app.js, style.css, ...)
├── android/                  # Proyek Android (Gradle)
│   ├── app/src/main/
│   │   ├── java/com/tikchat/app/
│   │   │   ├── MainActivity.kt    # WebView + izin + file chooser
│   │   │   ├── BackendService.kt  # Foreground service: jalankan biner Go
│   │   │   └── TtsBridge.kt       # window.AndroidTTS (WebView tak punya speechSynthesis)
│   │   ├── res/                   # layout, theme, ikon
│   │   └── assets/arm64-v8a/      # <-- biner Go ditaruh CI di sini (git-ignored)
│   └── gradlew
└── .github/workflows/build-apk.yml
```

## Cara kerja APK

1. `BackendService` (foreground service) mengekstrak biner Go dari assets lalu
   menjalankannya: `tikchat -daemon -datadir <app-files>/data` → listen di
   `127.0.0.1:5658`.
2. `MainActivity` menunggu backend siap lalu memuat WebUI di WebView.
3. TTS komentar memakai `TtsBridge` native (`window.AndroidTTS`) karena WebView
   tidak mendukung `speechSynthesis`.
4. Mic → speaker dan pemutar MP3 memakai Web Audio API di WebView
   (izin mic & file chooser ditangani aplikasi).

## Build di GitHub (disarankan)

1. Push repo ini ke GitHub.
2. Buka tab **Actions** → workflow **Build TikChat APK** jalan otomatis
   (atau tekan **Run workflow**).
3. Download artifact **tikchat-v1.4.9-apk** → install di HP
   (debug build, cukup untuk pemakaian pribadi).

Workflow: build backend Go (`GOOS=android GOARCH=arm64`) → taruh di
`android/app/src/main/assets/arm64-v8a/tikchat` → `./gradlew assembleDebug`.

## Build lokal (opsional)

Butuh: Go ≥ 1.23, JDK 17, Android SDK (platform 34, build-tools 34).

```sh
# 1. Backend
cd backend
GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w -X main.appVersion=v1.4.9" \
  -o ../android/app/src/main/assets/arm64-v8a/tikchat .

# 2. APK
cd ../android
./gradlew assembleDebug
# hasil: app/build/outputs/apk/debug/app-debug.apk
```

## Catatan

- Username tersimpan & data backend ada di penyimpanan privat aplikasi
  (`/data/data/com.tikchat.app/files/data`) — ikut terhapus saat uninstall.
- Lagu MP3 tersimpan di IndexedDB WebView (privat aplikasi).
- Aplikasi harus tetap berjalan (foreground) agar pantauan live tidak mati.
- Hanya arm64 yang didukung (biner Go di-build untuk `arm64-v8a`).
