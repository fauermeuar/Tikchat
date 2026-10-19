# ebpf-cgroup: inbound eBPF cgroup untuk mihomo (trafik HP sendiri)

## Arsitektur (2 komponen terpisah)

```
[HP - trafik aplikasi sendiri, TCP]
    |
    | cgroup/connect hook (eBPF)  <-- KOMPONEN 1 (kit ini)
    | belokkan -> 127.0.0.1:redir-port
    | SO_ORIGINAL_DST dipalsukan via cgroup/getsockopt
    v
[mihomo redir-port] --> routing normal --> proxy / direct

[Hotspot - klien tethering]
    |
    | TC ingress di ap0 (PR #3175)  <-- KOMPONEN 2 (sudah di binary)
    v
[mihomo tproxy-port]

[HP - UDP] --> iptables TPROXY (tetap, cgroup-eBPF TCP saja)
```

## Komponen 1: ebpf-cgroup (kit ini)

Package Go `listener/ebpfcgroup/` + BPF C `ebpf/redirect.c`, terintegrasi
native ke mihomo sebagai seksi config:

```yaml
ebpf-cgroup:
  enable: true
  redir-port: 7891        # harus sama dengan redir-port mihomo
  # cgroup-path: /sys/fs/cgroup   # opsional, default /sys/fs/cgroup
```

Cara kerja BPF:
- `cgroup/connect4` & `connect6`: intersep TCP connect() dari aplikasi
  (UID != 0, bukan loopback). Simpan tujuan asli di map LRU
  (key = socket cookie), belokkan ke 127.0.0.1:redir-port.
- `cgroup/getsockopt`: saat mihomo redir-port tanya SO_ORIGINAL_DST,
  kembalikan tujuan asli dari map. Kernel di-skip (return 0).
- Bypass: UID 0 (root: mihomo & loader sendiri, cegah loop),
  127.0.0.0/8, ::1, dan non-TCP.

## Komponen 2: ebpf PR #3175 (hotspot)

Sudah ada di binary (merge otomatis oleh workflow). Aktifkan via:

```yaml
ebpf:
  enable: true
  lan-interface: ["ap0"]   # HARUS ada; hotspot wajib nyala
  tproxy-port: 15359
```

CATATAN: kalau hotspot mati (ap0 hilang), mihomo GAGAL START.
Untuk HP harian, hotspot lebih aman tetap via iptables tproxy.

## Cara pasang ke fork

Lihat `apply-ebpf-cgroup.sh`, atau full-otomatis via workflow
`.github/workflows/build-android-arm64.yml` (butuh clang di runner
untuk compile BPF — sudah termasuk di workflow).

## Cara tes (di HP, sebagai root)

1. Pastikan mihomo config punya `redir-port` (mis. 7891) dan
   `ebpf-cgroup.enable: true`.
2. Jalankan mihomo, cek log: tidak ada error "ebpf-cgroup".
3. Cek program ter-attach:
   ```
   ls /sys/fs/cgroup/            # pastikan cgroup v2
   bpftool prog show 2>/dev/null | grep -i cg_
   ```
   (bpftool biasanya tidak ada di HP; alternatif: cek log kernel
   `dmesg | grep -i bpf` setelah connect aplikasi.)
4. Tes fungsional: buka aplikasi (bukan browser sistem), cek di
   zashboard/connections — koneksinya harus tercatat dengan tujuan
   ASLI (bukan 127.0.0.1). Itu bukti SO_ORIGINAL_DST palsu bekerja.
5. Tes bypass: koneksi mihomo sendiri ke server proxy harus tetap
   jalan (tidak loop).

## Jika gagal

Kemungkinan penyebab, berurutan:
1. **SELinux menolak syscall BPF** — cek `dmesg | grep -i avc`.
   Ini penolakan paling umum di Android.
2. **Bukan cgroup v2** — cek `mount | grep cgroup`, sesuaikan
   `cgroup-path`.
3. **Verifier menolak program** — cek `dmesg`, kirim lognya.
4. **UID mihomo bukan 0** — sesuaikan bypass di redirect.c.

Kirim log error ke pengembang untuk iterasi berikutnya.
