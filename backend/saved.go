// Daftar username TikTok tersimpan — persist di dataDir/saved_usernames.json.
//
// PENTING (bug v1.1.2): daemon turun ke nobody, tapi /data/adb tak bisa
// ditembus oleh nobody (induknya 700 milik root) — tulis via path SELALU
// gagal dengan permission denied. Solusi: file DIBUKA sebagai root SEBELUM
// setuid (pola yang sama dipakai file log), lalu semua baca/tulis lewat fd
// yang tetap valid setelah hak akses diturunkan. Tanpa fd, daftar hanya
// hidup di memori dan "hilang" tiap reboot.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type savedNames struct {
	mu   sync.Mutex
	f    *os.File // dibuka sbg root sebelum setuid; nil = degradasi memori-only
	path string
	list []string
}

// openSavedFile membuka (atau membuat) file penyimpanan. WAJIB dipanggil
// sebagai root, SEBELUM dropPrivileges.
func openSavedFile(dataDir string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dataDir, "saved_usernames.json"), os.O_RDWR|os.O_CREATE, 0o600)
}

func newSavedNames(f *os.File) *savedNames {
	s := &savedNames{f: f}
	if f != nil {
		s.path = f.Name()
	}
	s.load()
	log.Printf("username tersimpan: %d dimuat dari %s", len(s.list), s.path)
	return s
}

// normName: rapikan input ("@Andi " -> "Andi").
func normName(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "@")
	return u
}

func (s *savedNames) load() {
	if s.f == nil {
		return
	}
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(s.f)
	if err != nil {
		return
	}
	var list []string
	if json.Unmarshal(data, &list) != nil {
		return
	}
	seen := map[string]bool{}
	for _, u := range list {
		if n := normName(u); n != "" && !seen[strings.ToLower(n)] {
			seen[strings.ToLower(n)] = true
			s.list = append(s.list, n)
		}
	}
}

// persist menulis daftar ke disk via fd (bukan via path — lihat catatan di
// atas). Dipanggil dengan s.mu sudah dikunci. Error dikembalikan DAN dicatat
// LOUD — jangan pernah diabaikan diam-diam.
func (s *savedNames) persist() error {
	if s.f == nil {
		err := fmt.Errorf("file penyimpanan tidak dibuka")
		log.Printf("PERINGATAN: gagal menyimpan %s: %v", s.path, err)
		return err
	}
	data, err := json.Marshal(s.list)
	if err != nil {
		return err
	}
	if err := s.f.Truncate(0); err != nil {
		log.Printf("PERINGATAN: gagal menyimpan %s: %v", s.path, err)
		return err
	}
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		log.Printf("PERINGATAN: gagal menyimpan %s: %v", s.path, err)
		return err
	}
	if _, err := s.f.Write(data); err != nil {
		log.Printf("PERINGATAN: gagal menyimpan %s: %v", s.path, err)
		return err
	}
	s.f.Sync()
	return nil
}

// List mengembalikan salinan daftar.
func (s *savedNames) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.list))
	copy(out, s.list)
	return out
}

// Add menambah username.
// (true, nil) = ditambahkan; (false, nil) = kosong/duplikat;
// (false, err) = gagal tulis ke disk (daftar di-rollback agar sama dgn disk).
func (s *savedNames) Add(u string) (bool, error) {
	n := normName(u)
	if n == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.list {
		if strings.EqualFold(e, n) {
			return false, nil
		}
	}
	s.list = append(s.list, n)
	if err := s.persist(); err != nil {
		s.list = s.list[:len(s.list)-1]
		return false, err
	}
	return true, nil
}

// Remove menghapus username (case-insensitive).
func (s *savedNames) Remove(u string) (bool, error) {
	n := normName(u)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.list {
		if strings.EqualFold(e, n) {
			old := append([]string{}, s.list...)
			s.list = append(s.list[:i], s.list[i+1:]...)
			if err := s.persist(); err != nil {
				s.list = old
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}
