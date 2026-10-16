package main

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCustomResolverAktif(t *testing.T) {
	if net.DefaultResolver == nil {
		t.Fatal("net.DefaultResolver nil")
	}
	r := net.DefaultResolver
	if !r.PreferGo {
		t.Fatal("PreferGo harus true")
	}
	if r.Dial == nil {
		t.Fatal("Dial kustom harus terpasang")
	}
}

func TestCustomResolverBisaResolve(t *testing.T) {
	// Butuh internet di mesin tes; skip bila tidak ada.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, "dns.google")
	if err != nil {
		t.Skipf("tanpa internet / DNS publik diblokir: %v", err)
	}
	if len(addrs) == 0 {
		t.Fatal("hasil resolve kosong")
	}
	t.Logf("dns.google -> %v", addrs)
}

func TestShortErr(t *testing.T) {
	long := "dial tcp: lookup tiktok.eulerstream.com on [::1]:53: read udp [::1]:49549->[::1]:53: read: connection refused (extra padding agar > 120 karakter ....................)"
	if s := shortErr(testErr(long)); s != "connection refused" {
		t.Fatalf("dapat %q, mau %q", s, "connection refused")
	}
	if s := shortErr(errTestShort); s != "x" {
		t.Fatalf("error pendek harus utuh, dapat %q", s)
	}
}

var errTestShort = testErr("x")

type testErr string

func (e testErr) Error() string { return string(e) }
