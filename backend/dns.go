// Resolver DNS khusus untuk daemon di Android.
//
// Masalah: resolver bawaan Go di HP tidak menemukan server DNS yang valid
// (tidak ada /etc/resolv.conf yang berguna) sehingga jatuh ke default
// 127.0.0.1:53 / [::1]:53 yang kosong -> "connection refused".
// Daemon jalan sebagai root, dan root dikecualikan dari DNS hijack
// (TaptapBox) agar tidak loop -> makin tidak ada DNS yang bisa dipakai.
//
// Solusi: timpa net.DefaultResolver agar seluruh proses (termasuk library
// pihak ketiga) memakai server DNS terpilih. Server dipilih oleh prober
// latar yang mengirim query DNS beneran (dial UDP selalu "sukses" walau
// server mati, jadi failover harus berbasis probe, bukan dial).
//
// Urutan preferensi:
//  1. 127.0.0.1:5353 = stub DNS TaptapBox bila sedang jalan (ikut aturan user)
//  2. 8.8.8.8:53, 1.1.1.1:53 = publik sebagai cadangan
package main

import (
	"context"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// bestDNS menyimpan server terpilih (string) dari prober latar.
var bestDNS atomic.Value

func init() {
	bestDNS.Store("8.8.8.8:53") // default aman sebelum probe pertama selesai
	go probeLoop()
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			srv, _ := bestDNS.Load().(string)
			if srv == "" {
				srv = "8.8.8.8:53"
			}
			var d net.Dialer
			return d.DialContext(ctx, network, srv)
		},
	}
}

// probeLoop memilih server DNS yang benar-benar menjawab, refresh berkala.
func probeLoop() {
	pickBestDNS()
	for range time.Tick(60 * time.Second) {
		pickBestDNS()
	}
}

func pickBestDNS() {
	for _, srv := range candidateServers() {
		if dnsAlive(srv) {
			bestDNS.Store(srv)
			return
		}
	}
	cands := candidateServers()
	bestDNS.Store(cands[len(cands)-1])
}

// candidateServers: stub TaptapBox -> DNS sistem (net.dns1..4 via getprop)
// -> publik. DNS sistem selalu bisa dipakai app normal, jadi cadangan kuat
// bila carrier memblokir DNS publik.
func candidateServers() []string {
	cands := []string{"127.0.0.1:5353"}
	seen := map[string]bool{"127.0.0.1:5353": true}
	for _, p := range []string{"net.dns1", "net.dns2", "net.dns3", "net.dns4"} {
		out, err := exec.Command("/system/bin/getprop", p).Output()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(out))
		if ip == "" || seen[ip] {
			continue
		}
		if net.ParseIP(ip) == nil {
			continue
		}
		srv := net.JoinHostPort(ip, "53")
		seen[srv] = true
		cands = append(cands, srv)
	}
	cands = append(cands, "8.8.8.8:53", "1.1.1.1:53")
	return cands
}

// resolveLog mencatat hasil resolve host + server DNS aktif (diagnostik:
// bandingkan IP yang dipakai daemon vs yang dipakai Chrome).
func resolveLog(host string) {
	srv, _ := bestDNS.Load().(string)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		log.Printf("dns %s via %s: GAGAL %v", host, srv, shortErr(err))
		return
	}
	log.Printf("dns %s via %s -> %v", host, srv, addrs)
}

// dnsAlive: kirim query DNS beneran ke srv, true bila dijawab < 3 detik.
func dnsAlive(srv string) bool {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", srv)
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := r.LookupHost(ctx, "dns.google")
	return err == nil && len(addrs) > 0
}
