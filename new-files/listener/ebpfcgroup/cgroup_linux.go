//go:build linux

package ebpfcgroup

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

//go:embed ebpf/redirect.bpf.o
var bpfObject []byte

// Config is the runtime configuration for the redirector.
type Config struct {
	Enable     bool
	RedirPort  uint16
	CgroupPath string
}

// Manager owns the cgroup-eBPF redirector lifecycle. Close detaches
// every program and releases the collection; it is safe to call twice.
type Manager struct {
	links []link.Link
	coll  *ebpf.Collection
}

// Start loads the BPF programs, configures the redirect port, and attaches
// connect4/connect6/getsockopt hooks to the cgroup hierarchy.
func Start(cfg Config) (m *Manager, err error) {
	if !cfg.Enable {
		return nil, errors.New("ebpf-cgroup is disabled")
	}
	if cfg.RedirPort == 0 {
		return nil, errors.New("ebpf-cgroup requires a non-zero redir-port")
	}
	cgroupPath := cfg.CgroupPath
	if cgroupPath == "" {
		cgroupPath = "/sys/fs/cgroup"
	}

	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(bpfObject))
	if err != nil {
		return nil, fmt.Errorf("ebpf-cgroup: load BPF spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("ebpf-cgroup: create BPF collection: %w", err)
	}
	m = &Manager{coll: coll}
	defer func() {
		if err != nil {
			_ = m.Close()
		}
	}()

	// Publish the redirect port to the BPF side (host byte order).
	cfgMap := coll.Maps["cfg_port"]
	if cfgMap == nil {
		return nil, errors.New("ebpf-cgroup: BPF map cfg_port not found")
	}
	var key uint32
	if err := cfgMap.Put(key, cfg.RedirPort); err != nil {
		return nil, fmt.Errorf("ebpf-cgroup: set redir port: %w", err)
	}

	attachments := []struct {
		progName string
		attach   ebpf.AttachType
	}{
		{"cg_connect4", ebpf.AttachCGroupInet4Connect},
		{"cg_connect6", ebpf.AttachCGroupInet6Connect},
		{"cg_getsockopt", ebpf.AttachCGroupGetsockopt},
	}
	for _, a := range attachments {
		prog := coll.Programs[a.progName]
		if prog == nil {
			return nil, fmt.Errorf("ebpf-cgroup: BPF program %s not found", a.progName)
		}
		l, err := link.AttachCgroup(link.CgroupOptions{
			Path:    cgroupPath,
			Attach:  a.attach,
			Program: prog,
		})
		if err != nil {
			return nil, fmt.Errorf("ebpf-cgroup: attach %s to %s: %w (kernel/SELinux/cgroup tidak mendukung?)", a.progName, cgroupPath, err)
		}
		m.links = append(m.links, l)
	}

	return m, nil
}

// Close detaches all programs and releases BPF resources.
func (m *Manager) Close() error {
	var firstErr error
	for _, l := range m.links {
		if err := l.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.links = nil
	if m.coll != nil {
		m.coll.Close()
		m.coll = nil
	}
	return firstErr
}
