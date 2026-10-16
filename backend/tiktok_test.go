package main

import (
	"testing"

	gtl "github.com/steampoweredtaco/gotiktoklive"
)

func TestWatcherBuffer(t *testing.T) {
	w := newWatcher()
	w.push("alice", "halo")
	w.push("bob", "berapa harganya?")
	w.push("alice", "cek keranjang kuning")

	if got := w.State().Comments; got != 3 {
		t.Fatalf("Comments = %d, mau 3", got)
	}
	all := w.CommentsSince(0)
	if len(all) != 3 {
		t.Fatalf("CommentsSince(0) = %d, mau 3", len(all))
	}
	if all[0].Seq != 1 || all[0].User != "alice" || all[0].Text != "halo" {
		t.Fatalf("komentar pertama salah: %+v", all[0])
	}
	part := w.CommentsSince(2)
	if len(part) != 1 || part[0].Seq != 3 {
		t.Fatalf("CommentsSince(2) salah: %+v", part)
	}
	if none := w.CommentsSince(99); len(none) != 0 {
		t.Fatalf("CommentsSince(99) harus kosong, dapat %d", len(none))
	}
}

func TestWatcherBufferCap(t *testing.T) {
	w := newWatcher()
	for i := 0; i < maxBuf+50; i++ {
		w.push("u", "x")
	}
	if len(w.buf) != maxBuf {
		t.Fatalf("buffer = %d, mau %d", len(w.buf), maxBuf)
	}
	if w.buf[0].Seq != 51 {
		t.Fatalf("seq tertua = %d, mau 51", w.buf[0].Seq)
	}
}

func TestTiktokErrorMapping(t *testing.T) {
	// pastikan tipe error ramah-tampil tetap memenuhi interface error
	var err error = tiktokError("@x sedang tidak live")
	if err.Error() == "" {
		t.Fatal("tiktokError.Error() kosong")
	}
}

// TestHandleEventChatValue: regresi bug v1.0.1 — library mengirim event
// sebagai VALUE (gtl.ChatEvent), bukan pointer. Pastikan komentar masuk.
func TestHandleEventChatValue(t *testing.T) {
	w := newWatcher()
	// tiru persis yang dikirim parseMsg: value, bukan pointer
	var ev gtl.Event = gtl.ChatEvent{
		Comment: "berapa harganya?",
		User:    &gtl.User{Username: "budi123", Nickname: "Budi"},
	}
	w.handleEvent(ev)
	got := w.CommentsSince(0)
	if len(got) != 1 {
		t.Fatalf("komentar tidak masuk (dapat %d)", len(got))
	}
	if got[0].Text != "berapa harganya?" || got[0].User != "Budi (@budi123)" {
		t.Fatalf("isi salah: %+v", got[0])
	}
}

func TestHandleEventViewersValue(t *testing.T) {
	w := newWatcher()
	var ev gtl.Event = gtl.ViewersEvent{Viewers: 42}
	w.handleEvent(ev)
	if w.State().Viewers != 42 {
		t.Fatalf("viewers = %d, mau 42", w.State().Viewers)
	}
}

// TestHandleEventDisconnect: DisconnectEvent (pointer maupun value) tidak
// boleh dianggap komentar dan tidak boleh mengubah status jadi "selesai".
func TestHandleEventDisconnect(t *testing.T) {
	for _, ev := range []gtl.Event{&gtl.DisconnectEvent{}, gtl.DisconnectEvent{}} {
		w := newWatcher()
		w.handleEvent(ev)
		if len(w.CommentsSince(0)) != 0 {
			t.Fatalf("DisconnectEvent %T bocor jadi komentar", ev)
		}
		if w.State().Err == "live selesai" {
			t.Fatalf("DisconnectEvent %T memvonis live selesai", ev)
		}
	}
}

// TestStopIdempotenDanKosongkanStatus: Stop berulang tidak boleh panic
// (double close channel) dan status harus kembali kosong.
func TestStopIdempotenDanKosongkanStatus(t *testing.T) {
	w := newWatcher()
	w.state.Username = "budi"
	w.Stop()
	w.Stop()
	if w.State().Username != "" {
		t.Fatalf("status tidak dikosongkan: %+v", w.State())
	}
}
