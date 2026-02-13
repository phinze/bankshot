package portbpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 -type port_event PortMonitor ../../../bpf/port_monitor.c -- -nostdinc -O2 -g
