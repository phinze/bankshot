// SPDX-License-Identifier: GPL-2.0 OR MIT

// Minimal type definitions — avoids depending on vmlinux.h or kernel headers.
// These match the kernel ABI for tracepoint/sock/inet_sock_set_state (stable
// since Linux 4.16).

typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;
typedef int __s32;

// BPF helpers and map definitions
#define SEC(NAME) __attribute__((section(NAME), used))
#define __uint(name, val) int(*name)[val]
#define __type(name, val) typeof(val) *name

enum bpf_map_type {
	BPF_MAP_TYPE_PERF_EVENT_ARRAY = 4,
};

// BPF helper function IDs
static long (*bpf_perf_event_output)(void *ctx, void *map, __u64 flags,
				     void *data, __u64 size) = (void *)25;
static __u64 (*bpf_get_current_pid_tgid)(void) = (void *)14;

// Map definition for perf event output
struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(__u32));
	__uint(value_size, sizeof(__u32));
} events SEC(".maps");

// TCP states we care about
#define TCP_LISTEN 10

// Tracepoint context for sock/inet_sock_set_state.
// Fields match /sys/kernel/debug/tracing/events/sock/inet_sock_set_state/format.
struct inet_sock_set_state_args {
	// common tracepoint header (8 bytes)
	__u16 common_type;
	__u8 common_flags;
	__u8 common_preempt_count;
	__s32 common_pid;

	const void *skaddr;
	__s32 oldstate;
	__s32 newstate;
	__u16 sport;
	__u16 dport;
	__u16 family;
	__u16 protocol;
	__u8 saddr[4];
	__u8 daddr[4];
	__u8 saddr_v6[16];
	__u8 daddr_v6[16];
};

// Event structure emitted to userspace via perf ring buffer.
struct port_event {
	__u32 pid;
	__u16 sport;
	__u16 family;
	__s32 old_state;
	__s32 new_state;
	__u8 saddr[4];     // IPv4 bind address
	__u8 saddr_v6[16]; // IPv6 bind address
};

SEC("tracepoint/sock/inet_sock_set_state")
int trace_inet_sock_set_state(struct inet_sock_set_state_args *ctx) {
	__s32 old_state = ctx->oldstate;
	__s32 new_state = ctx->newstate;

	// Only care about transitions involving TCP_LISTEN
	if (old_state != TCP_LISTEN && new_state != TCP_LISTEN)
		return 0;

	__u64 pid_tgid = bpf_get_current_pid_tgid();
	__u32 pid = pid_tgid >> 32;

	struct port_event evt = {
		.pid = pid,
		.sport = ctx->sport,
		.family = ctx->family,
		.old_state = old_state,
		.new_state = new_state,
	};
	__builtin_memcpy(evt.saddr, ctx->saddr, 4);
	__builtin_memcpy(evt.saddr_v6, ctx->saddr_v6, 16);

	bpf_perf_event_output(ctx, &events, 0xffffffffULL, &evt, sizeof(evt));
	return 0;
}

// Force BTF emission for bpf2go -type port_event.
struct port_event *unused_port_event __attribute__((unused));

char _license[] SEC("license") = "Dual MIT/GPL";
