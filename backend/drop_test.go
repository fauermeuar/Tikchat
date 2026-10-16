package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// uidOf mengembalikan pemilik numerik sebuah path.
func uidOf(t *testing.T, p string) int {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("bukan Stat_t")
	}
	return int(st.Uid)
}

func TestChownDir(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("chown ke uid lain butuh root")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "log")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "x.log")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	chownDir(dir, 9999, 9999)
	for _, p := range []string{dir, sub, f} {
		if u := uidOf(t, p); u != 9999 {
			t.Fatalf("%s uid=%d, mau 9999", p, u)
		}
	}
}

// dropPrivileges TIDAK di-test di sini: bila test jalan sebagai root,
// setuid akan melumpuhkan proses test permanen. Perilakunya diverifikasi
// on-device via baris log "uid=9999".
