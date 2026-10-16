package main

import (
	"os"
	"path/filepath"
	"testing"
)

// openTestSaved membuka file penyimpanan di dir temp (simulasi openSavedFile).
func openTestSaved(t *testing.T, dir string) *os.File {
	t.Helper()
	f, err := openSavedFile(dir)
	if err != nil {
		t.Fatalf("openSavedFile: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestSavedAddDedupe(t *testing.T) {
	dir := t.TempDir()
	s := newSavedNames(openTestSaved(t, dir))
	if ok, err := s.Add("@AndiKindang18"); !ok || err != nil {
		t.Fatal("Add pertama harus true")
	}
	if ok, _ := s.Add("andikindang18"); ok {
		t.Fatal("duplikat (beda kapital) harus false")
	}
	if ok, _ := s.Add("  @ANDIKINDANG18  "); ok {
		t.Fatal("duplikat (spasi+kapital) harus false")
	}
	for _, u := range []string{"", "   ", "@"} {
		if ok, _ := s.Add(u); ok {
			t.Fatal("username kosong harus false")
		}
	}
	got := s.List()
	if len(got) != 1 || got[0] != "AndiKindang18" {
		t.Fatalf("list salah: %v", got)
	}
}

func TestSavedRemove(t *testing.T) {
	dir := t.TempDir()
	s := newSavedNames(openTestSaved(t, dir))
	s.Add("andi")
	s.Add("budi")
	if ok, err := s.Remove("@ANDI"); !ok || err != nil {
		t.Fatal("Remove harus true (case-insensitive)")
	}
	if ok, _ := s.Remove("andi"); ok {
		t.Fatal("Remove kedua harus false")
	}
	got := s.List()
	if len(got) != 1 || got[0] != "budi" {
		t.Fatalf("list salah: %v", got)
	}
}

func TestSavedPersist(t *testing.T) {
	dir := t.TempDir()
	s := newSavedNames(openTestSaved(t, dir))
	s.Add("andi")
	s.Add("budi")
	if _, err := os.Stat(filepath.Join(dir, "saved_usernames.json")); err != nil {
		t.Fatalf("file tidak tertulis: %v", err)
	}
	// instance baru (fd baru, simulasi restart daemon) harus baca ulang
	f2, err := openSavedFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	s2 := newSavedNames(f2)
	got := s2.List()
	if len(got) != 2 || got[0] != "andi" || got[1] != "budi" {
		t.Fatalf("reload salah: %v", got)
	}
	// file korup tidak boleh bikin panic, daftar jadi kosong
	f2.Truncate(0)
	f2.Seek(0, 0)
	f2.Write([]byte("{rusak"))
	s3 := newSavedNames(f2)
	if len(s3.List()) != 0 {
		t.Fatalf("file korup harus hasilkan list kosong: %v", s3.List())
	}
}

// TestSavedPersistError: regresi bug v1.1.1/v1.1.2 — bila fd tak tersedia
// (gagal dibuka), Add/Remove harus LAPORKAN error (bukan sukses palsu) dan
// daftar di-rollback agar sama dengan isi disk.
func TestSavedPersistError(t *testing.T) {
	s := newSavedNames(nil)
	if ok, err := s.Add("andi"); err == nil || ok {
		t.Fatalf("Add harus kembalikan error bila fd nil (ok=%v err=%v)", ok, err)
	}
	if len(s.List()) != 0 {
		t.Fatal("daftar harus rollback (tetap kosong) bila tulis gagal")
	}
	if ok, err := s.Remove("andi"); err != nil || ok {
		t.Fatalf("Remove username tak ada: ok=%v err=%v", ok, err)
	}
}
