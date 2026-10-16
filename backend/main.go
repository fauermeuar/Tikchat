// TikChat: pantau komentar TikTok LIVE tanpa membuka video (hemat kuota).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

var appVersion = "v1.4.9"

var (
	dataDir string
	logFile *os.File
)

// appUID: daemon turun ke nobody agar trafiknya ikut VPN. Aturan netfilter
// TaptapBox (mode TProxy) mengecualikan uid 0/root dari redirect agar tidak
// loop — akibatnya daemon yang jalan sebagai root SELALU direct via carrier
// dan kena filter SNI ISP (gejala: TLS handshake timeout ke eulerstream,
// sementara Chrome yang ikut VPN baik-baik saja).
const appUID = 9999 // nobody

// maxLogSize: log lebih besar dari ini dirotasi (jadi .old) saat daemon mulai,
// agar /data tidak penuh oleh log yang tumbuh tanpa batas.
const maxLogSize = 1 << 20

func rotateLog(path string) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		os.Rename(path, path+".old")
	}
}

// chownDir: ganti pemilik seluruh isi dir (tanpa mengikuti symlink).
// Mengembalikan error pertama — kegagalan chown TIDAK boleh diam-diam:
// bila dir tetap milik root, daemon yang sudah setuid ke nobody tak bisa
// tulis file baru (username tersimpan "hilang" tiap reboot — bug v1.1.1).
func chownDir(dir string, uid, gid int) error {
	var firstErr error
	filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", p, err)
			}
			return nil
		}
		if e := os.Lchown(p, uid, gid); e != nil && firstErr == nil {
			firstErr = fmt.Errorf("lchown %s: %w", p, e)
		}
		return nil
	})
	return firstErr
}

// probeWrite: uji tulis LANGSUNG (via path) ke dataDir sesudah hak akses
// diturunkan. Sejak v1.1.3 persistensi memakai fd yang dibuka sbg root, jadi
// kegagalan di sini bukan lagi fatal — tapi tetap dicatat sebagai info
// diagnostik (mis. /data/adb tak bisa ditembus nobody).
func probeWrite(dir string) {
	p := filepath.Join(dir, ".writetest")
	if err := os.WriteFile(p, []byte("ok"), 0o644); err != nil {
		log.Printf("catatan: uji tulis langsung %s sbg uid=%d gagal (%v) — dipakai jalur fd", dir, os.Getuid(), err)
		return
	}
	os.Remove(p)
	log.Printf("uji tulis %s OK (uid=%d)", dir, os.Getuid())
}

// dropPrivileges: bila jalan sebagai root, serahkan dataDir ke nobody lalu
// setuid/setgid ke nobody. Kegagalan bersifat non-fatal (lanjut sebagai root
// = perilaku lama) tapi dicatat LOUD di log agar penyebabnya terlihat.
func dropPrivileges(dir string) {
	if os.Getuid() != 0 {
		log.Printf("uid=%d (bukan root): hak akses tidak diubah", os.Getuid())
		return
	}
	if err := chownDir(dir, appUID, appUID); err != nil {
		log.Printf("PERINGATAN: chown %s ke uid %d gagal: %v", dir, appUID, err)
	}
	if err := syscall.Setgid(appUID); err != nil {
		log.Printf("PERINGATAN: setgid(%d) gagal: %v — lanjut sebagai root", appUID, err)
		return
	}
	if err := syscall.Setuid(appUID); err != nil {
		log.Printf("PERINGATAN: setuid(%d) gagal: %v — lanjut sebagai root (trafik tak ikut VPN)", appUID, err)
		return
	}
	log.Printf("hak akses diturunkan ke uid %d (nobody) — trafik ikut VPN", appUID)
}

func main() {
	daemon := flag.Bool("daemon", false, "jalankan sebagai daemon")
	showVer := flag.Bool("version", false, "tampilkan versi")
	dir := flag.String("datadir", "/data/adb/tikchat", "direktori data")
	flag.Parse()

	if *showVer {
		fmt.Println(appVersion)
		return
	}
	dataDir = *dir
	if err := os.MkdirAll(filepath.Join(dataDir, "log"), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	logPath := filepath.Join(dataDir, "log", "tikchat.log")
	rotateLog(logPath)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("buka log: %v", err)
	}
	logFile = lf
	defer lf.Close()
	log.SetOutput(lf)
	log.SetFlags(log.LstdFlags)

	// Buka file persistensi SEBELUM turun hak akses (sebagai root):
	// setelah setuid ke nobody, /data/adb tak bisa ditembus lagi sehingga
	// buka via path akan gagal — fd yang sudah dibuka tetap valid (pola yang
	// sama dengan file log). Kegagalan di sini non-fatal: daftar hanya hidup
	// di memori sesi itu, tapi dicatat LOUD.
	savedFile, err := openSavedFile(dataDir)
	if err != nil {
		log.Printf("PERINGATAN: tak bisa buka %s/saved_usernames.json: %v — username tersimpan hanya di memori",
			dataDir, err)
	} else {
		defer savedFile.Close()
	}

	// turunkan hak akses SETELAH log siap: fd log tetap valid setelah
	// chown+setuid, dan bila setuid gagal, penyebabnya tercatat di log
	// (tidak mati diam-diam seperti v1.0.6).
	dropPrivileges(dataDir)
	probeWrite(dataDir)

	if !*daemon {
		fmt.Printf("TikChat %s — pakai -daemon untuk jalan sebagai service\n", appVersion)
		return
	}

	log.Printf("TikChat %s mulai (datadir=%s uid=%d)", appVersion, dataDir, os.Getuid())
	w := newWatcher()
	saved := newSavedNames(savedFile)
	srv := newServer(w, saved)

	// berhenti rapi saat modul dimatikan/di-update (SIGTERM dari pkill)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		log.Printf("sinyal berhenti diterima, menutup server")
		srv.shutdown()
	}()

	if err := srv.listenAndServe("127.0.0.1:5658"); err != nil {
		log.Fatalf("server: %v", err)
	}
	log.Printf("TikChat berhenti")
}
