/* redirect.c — cgroup-eBPF TCP redirector PoC untuk Android.
 *
 * Cara kerja:
 *   1. cgroup/connect4 & connect6: intersep TCP connect() dari aplikasi,
 *      simpan tujuan asli di map (key = socket cookie), lalu belokkan
 *      tujuan ke 127.0.0.1:<redir_port>.
 *   2. cgroup/getsockopt: palsukan jawaban SO_ORIGINAL_DST agar
 *      mihomo redir-port mendapatkan tujuan asli.
 *
 * Yang dilewat (bypass):
 *   - UID 0 (root): mihomo & loader jalan sebagai root, cegah loop.
 *   - 127.0.0.0/8 dan ::1: cegah loop lokal.
 *   - Non-TCP: hanya TCP yang ditangani PoC ini.
 *
 * Build (butuh clang dengan target BPF, mis. GitHub Actions ubuntu-latest):
 *   clang -O2 -target bpf -c redirect.c -o redirect.bpf.o
 *
 * SPDX-License-Identifier: GPL-2.0-only
 */

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#ifndef IPPROTO_TCP
#define IPPROTO_TCP 6
#endif
#ifndef AF_INET
#define AF_INET 2
#endif
#ifndef AF_INET6
#define AF_INET6 10
#endif
#ifndef SOL_IP
#define SOL_IP 0
#endif
#ifndef SO_ORIGINAL_DST
#define SO_ORIGINAL_DST 80
#endif

/* Tujuan asli yang disimpan per-socket. */
struct orig_dst {
	__u8  family;      /* AF_INET=2 atau AF_INET6=10 */
	__u8  pad[3];
	__u16 port;        /* network byte order */
	__u8  addr[16];    /* IPv4: 4 byte pertama; IPv6: 16 byte */
};

/* key = socket cookie (stabil per-socket, sama di connect & getsockopt). */
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 16384);
	__type(key, __u64);
	__type(value, struct orig_dst);
} orig_dst_map SEC(".maps");

/* Port redir tujuan (HOST byte order), diisi loader via map update. */
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, __u16);
} cfg_port SEC(".maps");

static __always_inline int redir_port(void)
{
	__u32 key = 0;
	__u16 *p = bpf_map_lookup_elem(&cfg_port, &key);
	if (!p || *p == 0)
		return 0;
	return *p;
}

static __always_inline int is_loopback4(__u32 daddr)
{
	/* daddr network order; oktet pertama = byte terendah di LE. */
	return (daddr & 0xff) == 0x7f;
}

SEC("cgroup/connect4")
int cg_connect4(struct bpf_sock_addr *ctx)
{
	struct orig_dst orig = {};
	__u64 cookie;
	int rport;

	if (ctx->protocol != IPPROTO_TCP)
		return 1;

	/* Lewati root (mihomo & loader) agar tidak loop. */
	if ((bpf_get_current_uid_gid() & 0xffffffff) == 0)
		return 1;

	if (is_loopback4(ctx->user_ip4))
		return 1;

	cookie = bpf_get_socket_cookie(ctx);
	if (!cookie)
		return 1;

	rport = redir_port();
	if (!rport)
		return 1; /* belum dikonfigurasi; lewatkan saja */

	orig.family = AF_INET;
	orig.port = ctx->user_port; /* sudah network byte order */
	__builtin_memcpy(orig.addr, &ctx->user_ip4, 4);
	bpf_map_update_elem(&orig_dst_map, &cookie, &orig, BPF_ANY);

	/* Belokkan ke 127.0.0.1:<rport> */
	ctx->user_ip4 = 0x0100007f; /* 127.0.0.1 network order */
	ctx->user_port = bpf_htons((__u16)rport);

	return 1;
}

SEC("cgroup/connect6")
int cg_connect6(struct bpf_sock_addr *ctx)
{
	struct orig_dst orig = {};
	__u64 cookie;
	int rport;
	int i;

	if (ctx->protocol != IPPROTO_TCP)
		return 1;

	if ((bpf_get_current_uid_gid() & 0xffffffff) == 0)
		return 1;

	/* Lewati ::1 */
	for (i = 0; i < 3; i++) {
		if (ctx->user_ip6[i] != 0)
			goto not_loopback;
	}
	if (ctx->user_ip6[3] == bpf_htonl(1))
		return 1;
not_loopback:

	cookie = bpf_get_socket_cookie(ctx);
	if (!cookie)
		return 1;

	rport = redir_port();
	if (!rport)
		return 1;

	orig.family = AF_INET6;
	orig.port = ctx->user_port;
	__builtin_memcpy(orig.addr, ctx->user_ip6, 16);
	bpf_map_update_elem(&orig_dst_map, &cookie, &orig, BPF_ANY);

	ctx->user_ip6[0] = 0;
	ctx->user_ip6[1] = 0;
	ctx->user_ip6[2] = 0;
	ctx->user_ip6[3] = bpf_htonl(1); /* ::1 */
	ctx->user_port = bpf_htons((__u16)rport);

	return 1;
}

/* Struktur minimal sockaddr untuk jawaban SO_ORIGINAL_DST. */
struct sa_in {
	__u16 sin_family;
	__u16 sin_port;
	__u32 sin_addr;
	__u8  sin_zero[8];
};

struct sa_in6 {
	__u16 sin6_family;
	__u16 sin6_port;
	__u32 sin6_flowinfo;
	__u8  sin6_addr[16];
	__u32 sin6_scope_id;
};

SEC("cgroup/getsockopt")
int cg_getsockopt(struct bpf_sockopt *ctx)
{
	struct orig_dst *orig;
	__u64 cookie;
	void *optval = (void *)(long)ctx->optval;
	void *optval_end = (void *)(long)ctx->optval_end;

	/* Hanya SO_ORIGINAL_DST di level SOL_IP. */
	if (ctx->level != SOL_IP || ctx->optname != SO_ORIGINAL_DST)
		return 1;

	cookie = bpf_get_socket_cookie(ctx);
	if (!cookie)
		return 1;

	orig = bpf_map_lookup_elem(&orig_dst_map, &cookie);
	if (!orig)
		return 1; /* bukan socket kita; biarkan kernel */

	if (orig->family == AF_INET) {
		struct sa_in *sa = optval;
		if (optval + sizeof(*sa) > optval_end)
			return 1;
		sa->sin_family = AF_INET;
		sa->sin_port = orig->port;
		__builtin_memcpy(&sa->sin_addr, orig->addr, 4);
		__builtin_memset(sa->sin_zero, 0, sizeof(sa->sin_zero));
		ctx->optlen = sizeof(*sa);
	} else if (orig->family == AF_INET6) {
		struct sa_in6 *sa6 = optval;
		if (optval + sizeof(*sa6) > optval_end)
			return 1;
		sa6->sin6_family = AF_INET6;
		sa6->sin6_port = orig->port;
		sa6->sin6_flowinfo = 0;
		__builtin_memcpy(sa6->sin6_addr, orig->addr, 16);
		sa6->sin6_scope_id = 0;
		ctx->optlen = sizeof(*sa6);
	} else {
		return 1;
	}

	ctx->retval = 0;
	return 0; /* 0 = tangani sendiri, jangan panggil kernel */
}

char LICENSE[] SEC("license") = "GPL";
