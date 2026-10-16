// TikTok LIVE watcher: konek ke websocket webcast TikTok cukup modal username,
// terima komentar real-time TANPA mengunduh video (hemat kuota).
//
// Protokol ditangani library gotiktoklive (MIT):
// username -> room_id -> signed WebSocket URL (via Euler Stream sign server)
// -> frame protobuf WebcastPushFrame -> event per tipe pesan.
package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	gtl "github.com/steampoweredtaco/gotiktoklive"
)

// Comment: satu komentar live.
type Comment struct {
	Seq  uint64 `json:"seq"`
	User string `json:"user"`
	Text string `json:"text"`
	Ts   int64  `json:"ts"`
}

// WatchState: status pantauan.
type WatchState struct {
	Username  string `json:"username"`
	RoomID    string `json:"room_id"`
	Connected bool   `json:"connected"`
	Live      bool   `json:"live"`
	Viewers   int    `json:"viewers"`
	Comments  int    `json:"comments"`
	StartedAt int64  `json:"started_at"`
	Err       string `json:"err,omitempty"`
}

const maxBuf = 500

type watcher struct {
	ctl   sync.Mutex // menserialkan Watch/Stop agar tidak balapan
	mu    sync.Mutex
	state WatchState
	buf   []Comment
	seq   uint64
	stop  chan struct{}
	wg    sync.WaitGroup
}

func newWatcher() *watcher { return &watcher{} }

// Watch mulai memantau live milik username (tanpa @ juga boleh).
func (w *watcher) Watch(username string) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" {
		return
	}
	w.ctl.Lock()
	defer w.ctl.Unlock()
	w.halt()
	stop := make(chan struct{})
	w.mu.Lock()
	w.state = WatchState{Username: username, StartedAt: time.Now().Unix()}
	w.buf = nil
	w.seq = 0
	w.stop = stop
	w.mu.Unlock()
	w.wg.Add(1)
	go w.loop(username, stop)
}

// Stop menghentikan pantauan dan mengosongkan status (aman dipanggil berulang).
func (w *watcher) Stop() {
	w.ctl.Lock()
	defer w.ctl.Unlock()
	w.halt()
	w.mu.Lock()
	w.state = WatchState{}
	w.mu.Unlock()
}

// halt menghentikan goroutine loop tanpa mengosongkan status.
// Pemanggil wajib memegang w.ctl.
func (w *watcher) halt() {
	w.mu.Lock()
	ch := w.stop
	w.stop = nil
	w.mu.Unlock()
	if ch != nil {
		close(ch)
		w.wg.Wait()
	}
	w.mu.Lock()
	w.state.Connected = false
	w.mu.Unlock()
}

// State mengembalikan status terkini.
func (w *watcher) State() WatchState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

// CommentsSince mengembalikan komentar dengan Seq > seq.
func (w *watcher) CommentsSince(seq uint64) []Comment {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []Comment
	for _, c := range w.buf {
		if c.Seq > seq {
			out = append(out, c)
		}
	}
	return out
}

func (w *watcher) push(user, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	w.buf = append(w.buf, Comment{Seq: w.seq, User: user, Text: text, Ts: time.Now().Unix()})
	if len(w.buf) > maxBuf {
		w.buf = w.buf[len(w.buf)-maxBuf:]
	}
	w.state.Comments = int(w.seq)
}

func (w *watcher) setErr(err string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.Err = err
}

func (w *watcher) setLive(roomID string, viewers int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.Live = true
	w.state.Connected = true
	w.state.RoomID = roomID
	w.state.Viewers = viewers
	w.state.Err = ""
}

func (w *watcher) setEnded() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.Live = false
	w.state.Connected = false
	w.state.Err = "live selesai"
}

// isLiveEnded mengecek apakah live milik username benar-benar sudah selesai.
// Dipakai saat channel event tertutup tiba-tiba. Bila pengecekan gagal
// (mis. jaringan bermasalah), anggap BELUM selesai agar loop() me-retry,
// bukan memvonis "live selesai" padahal live masih jalan.
func (w *watcher) isLiveEnded(username string) bool {
	tt, err := gtl.NewTikTok()
	if err != nil {
		return false
	}
	ui, err := tt.GetUserInfo(username)
	if err != nil {
		if errors.Is(err, gtl.ErrUserOffline) {
			return true
		}
		return false
	}
	return ui.RoomID == ""
}

// loop menjalankan koneksi; reconnect ringan bila putus di tengah jalan.
func (w *watcher) loop(username string, stop <-chan struct{}) {
	defer w.wg.Done()

	backoff := 5 * time.Second
	for {
		started := time.Now()
		err := w.connectLive(username, stop)
		if time.Since(started) > 30*time.Second {
			backoff = 5 * time.Second // sesi tadi stabil: mulai lagi dari awal
		}
		if err == nil {
			return // stop diminta, atau live selesai
		}
		// error fatal (offline/username salah): jangan retry membabi buta
		var te tiktokError
		if errors.As(err, &te) {
			log.Printf("watch %s: %v", username, err)
			w.setErr(err.Error())
			return
		}
		log.Printf("watch %s putus: %v — coba lagi %v", username, err, backoff)
		resolveLog("tiktok.eulerstream.com")
		w.setErr(fmt.Sprintf("koneksi putus (%v), mencoba lagi…", shortErr(err)))
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
}

// connectLive konek ke TikTok LIVE dan teruskan komentar ke push().
// Return nil bila berhenti normal (stop / live selesai).
func (w *watcher) connectLive(username string, stop <-chan struct{}) error {
	tt, err := gtl.NewTikTok()
	if err != nil {
		return err
	}
	live, err := tt.TrackUser(username)
	if err != nil {
		if errors.Is(err, gtl.ErrUserOffline) || errors.Is(err, gtl.ErrLiveHasEnded) {
			return tiktokError("@" + username + " sedang tidak live (atau username salah)")
		}
		return err
	}
	defer live.Close()

	w.setLive(live.ID, 0)
	log.Printf("terhubung ke live @%s (room %s)", username, live.ID)

	for {
		select {
		case <-stop:
			return nil
		case ev, ok := <-live.Events:
			if !ok {
				// Channel ditutup library untuk DUA alasan: live benar-benar
				// selesai, ATAU koneksi putus sesaat (jaringan/VPN goyang).
				// Jangan langsung vonis "live selesai" — verifikasi dulu.
				if w.isLiveEnded(username) {
					w.setEnded()
					return nil
				}
				log.Printf("channel event tertutup tapi @%s masih live — reconnect", username)
				return fmt.Errorf("koneksi ke live terputus")
			}
			w.handleEvent(ev)
		}
	}
}

// handleEvent memproses satu event dari library.
// CATATAN: gotiktoklive mengirim event sebagai VALUE (bukan pointer),
// mis. gtl.ChatEvent — bukan *gtl.ChatEvent. Salah pakai pointer bikin
// case tidak pernah cocok dan komentar "hilang" diam-diam (bug v1.0.1).
func (w *watcher) handleEvent(ev gtl.Event) {
	switch e := ev.(type) {
	case gtl.ChatEvent:
		var name string
		if e.User != nil {
			name = e.User.Nickname
			if name == "" {
				name = e.User.Username
			}
			if e.User.Username != "" && e.User.Username != name {
				name += " (@" + e.User.Username + ")"
			}
		}
		if name == "" {
			name = "?"
		}
		if strings.TrimSpace(e.Comment) != "" {
			w.push(name, e.Comment)
		}
	case gtl.ViewersEvent:
		w.mu.Lock()
		w.state.Viewers = e.Viewers
		w.mu.Unlock()
	case *gtl.DisconnectEvent, gtl.DisconnectEvent:
		// Websocket terputus; loop() akan verifikasi status live lalu
		// reconnect bila live masih jalan. (Library mengirimnya sebagai
		// pointer; value ikut ditangani untuk jaga-jaga.)
		log.Printf("tikchat: websocket terputus, verifikasi status live…")
	default:
		log.Printf("tikchat: event tak ditangani %T", ev)
	}
}

type tiktokError string

func (e tiktokError) Error() string { return string(e) }

// shortErr memadatkan error jaringan agar muat di UI.
func shortErr(err error) string {
	s := err.Error()
	// potong detail dial yang panjang, ambil intinya
	if i := len(s); i > 120 {
		// cari pola umum: "dial tcp:" / "lookup"
		for _, p := range []string{"connection refused", "timeout", "no such host", "network is unreachable"} {
			if strings.Contains(s, p) {
				return p
			}
		}
		return s[:117] + "…"
	}
	return s
}
