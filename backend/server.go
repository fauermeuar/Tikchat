// HTTP server TikChat: WebUI di /tikchat/, API di /api/*.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed webui
var webuiFS embed.FS

// usernameRe: aturan username TikTok (huruf, angka, titik, garis bawah; maks 24).
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._]{1,24}$`)

// csp: WebUI hanya memuat aset dari origin sendiri (tanpa inline script).
// media-src blob: dibuka agar pemutar musik bisa memutar lagu dari IndexedDB.
const csp = "default-src 'self'; img-src 'self' data:; script-src 'self'; " +
	"style-src 'self'; media-src 'self' blob:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".json": "application/manifest+json; charset=utf-8",
	".png":  "image/png",
}

type server struct {
	w     *watcher
	saved *savedNames
	mux   *http.ServeMux
	srv   *http.Server
}

func newServer(w *watcher, saved *savedNames) *server {
	s := &server{w: w, saved: saved, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/watch", s.handleWatch)
	s.mux.HandleFunc("/api/stop", s.handleStop)
	s.mux.HandleFunc("/api/comments", s.handleComments)
	s.mux.HandleFunc("/api/usernames", s.handleUsernames)
	s.mux.HandleFunc("/tikchat/", s.handleWebUI)
	s.mux.HandleFunc("/", func(wr http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(wr, r)
			return
		}
		http.Redirect(wr, r, "/tikchat/", http.StatusFound)
	})
	s.srv = &http.Server{
		Handler:           s.guard(s.mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

func (s *server) listenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("WebUI: http://%s/tikchat/", addr)
	err = s.srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// shutdown menutup server dengan rapi (dipanggil saat SIGTERM/SIGINT).
func (s *server) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}

// hostOnly: ambil nama host tanpa port/kurung, huruf kecil.
func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.Trim(h, "[]"))
}

func isLocalHost(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// guard: hanya dari perangkat ini. Host dicek (anti DNS-rebinding) dan Origin
// dicek (anti CSRF: situs lain di browser tidak boleh menembak API lokal).
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(hostOnly(r.Host)) {
			http.Error(w, "hanya dari perangkat ini", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || !isLocalHost(hostOnly(u.Host)) {
				http.Error(w, "origin ditolak", http.StatusForbidden)
				return
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "version": appVersion, "watch": s.w.State()})
}

// readUsername membaca body JSON {"username": "..."} dan memvalidasinya.
// Mengembalikan username bersih, atau string kosong + pesan error.
func readUsername(r *http.Request) (string, string) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	var req struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(body, &req); err != nil || normName(req.Username) == "" {
		return "", "username kosong"
	}
	u := normName(req.Username)
	if !usernameRe.MatchString(u) {
		return "", "username tidak valid (huruf, angka, titik, garis bawah; maks 24)"
	}
	return u, ""
}

func (s *server) handleWatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "pakai POST", http.StatusMethodNotAllowed)
		return
	}
	u, errMsg := readUsername(r)
	if errMsg != "" {
		writeJSON(w, map[string]any{"ok": false, "err": errMsg})
		return
	}
	s.w.Watch(u)
	writeJSON(w, map[string]any{"ok": true, "watch": s.w.State()})
}

func (s *server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "pakai POST", http.StatusMethodNotAllowed)
		return
	}
	s.w.Stop()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *server) handleComments(w http.ResponseWriter, r *http.Request) {
	var seq uint64
	if q := r.URL.Query().Get("after"); q != "" {
		if n, err := strconv.ParseUint(q, 10, 64); err == nil {
			seq = n
		}
	}
	writeJSON(w, map[string]any{"ok": true, "comments": s.w.CommentsSince(seq)})
}

// handleUsernames: GET daftar, POST tambah, DELETE hapus username tersimpan.
func (s *server) handleUsernames(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"ok": true, "usernames": s.saved.List()})
	case http.MethodPost:
		u, errMsg := readUsername(r)
		if errMsg != "" {
			writeJSON(w, map[string]any{"ok": false, "err": errMsg})
			return
		}
		if _, err := s.saved.Add(u); err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": "gagal menyimpan: " + err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "usernames": s.saved.List()})
	case http.MethodDelete:
		if normName(r.URL.Query().Get("username")) == "" {
			writeJSON(w, map[string]any{"ok": false, "err": "username kosong"})
			return
		}
		if _, err := s.saved.Remove(r.URL.Query().Get("username")); err != nil {
			writeJSON(w, map[string]any{"ok": false, "err": "gagal menyimpan: " + err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "usernames": s.saved.List()})
	default:
		http.Error(w, "metode tak didukung", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleWebUI(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/tikchat/")
	if p == "" {
		p = "index.html"
	}
	// cegah traversal
	if strings.Contains(p, "..") {
		http.NotFound(w, r)
		return
	}
	data, err := webuiFS.ReadFile("webui/" + p)
	if err != nil {
		// fallback SPA ke index.html
		data, err = webuiFS.ReadFile("webui/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		p = "index.html"
	}
	h := w.Header()
	if ct, ok := contentTypes[strings.ToLower(path.Ext(p))]; ok {
		h.Set("Content-Type", ct)
	}
	h.Set("Content-Security-Policy", csp)
	// UI selalu divalidasi ulang agar update modul langsung terlihat.
	h.Set("Cache-Control", "no-cache")
	w.Write(data)
}
