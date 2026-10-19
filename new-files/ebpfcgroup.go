package config

// EbpfCgroup is the configuration for the cgroup-eBPF TCP redirector.
//
// Unlike the "ebpf" inbound (PR #3175, gateway/LAN oriented), this captures
// the phone's OWN TCP traffic via cgroup connect hooks and redirects it to
// mihomo's redir-port. The original destination is recovered through a
// faked SO_ORIGINAL_DST answer, so the stock redir listener works unchanged.
//
// Hotspot clients are NOT captured here (cgroup hooks only see local
// processes); they stay on iptables TPROXY or PR #3175's ebpf inbound.
type EbpfCgroup struct {
	Enable     bool   `yaml:"enable" json:"enable"`
	RedirPort  uint16 `yaml:"redir-port" json:"redir-port"`
	CgroupPath string `yaml:"cgroup-path" json:"cgroup-path"`
}

// Equal compares two EbpfCgroup configs for hot-reload decisions.
func (e *EbpfCgroup) Equal(other EbpfCgroup) bool {
	return e.Enable == other.Enable &&
		e.RedirPort == other.RedirPort &&
		e.CgroupPath == other.CgroupPath
}
