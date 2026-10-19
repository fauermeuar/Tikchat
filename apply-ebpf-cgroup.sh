#!/bin/sh
# apply-ebpf-cgroup.sh — pasang inbound ebpf-cgroup ke fork mihomo.
# Jalankan di root repo mihomo (yang sudah merge PR #3175, mis. via
# workflow build-android-arm64.yml dari kit sebelumnya, atau manual).
#
# Isi kit ini:
#   new-files/listener/ebpfcgroup/   -> copy ke listener/ebpfcgroup/
#   new-files/ebpfcgroup.go          -> copy ke listener/config/ebpfcgroup.go
#   ebpfcgroup.patch                 -> git apply ke config/config.go & hub/executor/executor.go
set -e
cd "$(dirname "$0")"

echo "== copy file baru =="
mkdir -p listener/ebpfcgroup/ebpf listener/config
cp -r new-files/listener/ebpfcgroup/* listener/ebpfcgroup/
cp new-files/ebpfcgroup.go listener/config/ebpfcgroup.go
echo "   listener/ebpfcgroup/{cgroup_linux.go,cgroup_other.go,ebpf/redirect.c}"
echo "   listener/config/ebpfcgroup.go"

echo "== apply patch config & executor =="
git apply --check ebpfcgroup.patch && git apply ebpfcgroup.patch
echo "   config/config.go + hub/executor/executor.go OK"

echo ""
echo "SELESAI. Langkah lanjut:"
echo "  1. Compile BPF: clang -O2 -target bpf -c listener/ebpfcgroup/ebpf/redirect.c -o listener/ebpfcgroup/ebpf/redirect.bpf.o"
echo "  2. Build: CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags \"-s -w\" -o mihomo ."
echo "  (Atau pakai workflow GitHub Actions di kit ini — otomatis.)"
