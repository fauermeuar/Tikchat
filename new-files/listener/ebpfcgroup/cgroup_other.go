//go:build !linux

package ebpfcgroup

import "errors"

// Manager is a stub on non-Linux platforms.
type Manager struct{}

// Config mirrors the Linux config for API compatibility.
type Config struct {
	Enable     bool
	RedirPort  uint16
	CgroupPath string
}

// Start always fails on non-Linux: cgroup-eBPF requires Linux.
func Start(Config) (*Manager, error) {
	return nil, errors.New("ebpf-cgroup requires Linux")
}

// Close is a no-op on non-Linux.
func (m *Manager) Close() error { return nil }
