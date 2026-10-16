package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	f, err := openSavedFile(dir)
	if err != nil {
		t.Fatalf("openSavedFile: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return newServer(newWatcher(), newSavedNames(f))
}

// TestGuardHostDanOrigin: Host harus lokal (anti DNS-rebinding) dan Origin,
// bila ada, juga harus lokal (anti CSRF dari situs lain).
func TestGuardHostDanOrigin(t *testing.T) {
	s := newTestServer(t)
	cases := []struct {
		host   string
		origin string
		want   int
	}{
		{"127.0.0.1:5658", "", http.StatusOK},
		{"localhost:5658", "", http.StatusOK},
		{"[::1]:5658", "", http.StatusOK},
		{"evil.example:5658", "", http.StatusForbidden},
		{"127.0.0.1:5658", "http://evil.example", http.StatusForbidden},
		{"127.0.0.1:5658", "null", http.StatusForbidden},
		{"127.0.0.1:5658", "http://127.0.0.1:5658", http.StatusOK},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		s.srv.Handler.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Fatalf("host=%q origin=%q: status %d, mau %d", c.host, c.origin, rec.Code, c.want)
		}
	}
}

// TestWatchUsernameTidakValid: username aneh ditolak sebelum menyentuh jaringan.
func TestWatchUsernameTidakValid(t *testing.T) {
	s := newTestServer(t)
	for _, u := range []string{"", "@", "a b", "../x", "toolongtoolongtoolongtoolong1"} {
		req := httptest.NewRequest(http.MethodPost, "/api/watch", strings.NewReader(`{"username":"`+u+`"}`))
		req.Host = "127.0.0.1:5658"
		rec := httptest.NewRecorder()
		s.srv.Handler.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), `"ok":false`) {
			t.Fatalf("username %q harus ditolak, dapat: %s", u, rec.Body.String())
		}
	}
}

func TestWebUIHeaderKeamanan(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/tikchat/", nil)
	req.Host = "127.0.0.1:5658"
	rec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("CSP tidak terpasang")
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content-type salah: %q", rec.Header().Get("Content-Type"))
	}
}
