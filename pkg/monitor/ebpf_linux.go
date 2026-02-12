package monitor

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/phinze/bankshot/pkg/monitor/portbpf"
)

const tcpListen = 10

// ebpfMonitor uses eBPF tracepoint/sock/inet_sock_set_state for instant
// edge-triggered port events. It implements PortEventSource.
type ebpfMonitor struct {
	events chan PortEvent
	logger *slog.Logger
}

// probeEBPF attempts to load and immediately close the eBPF program to test
// whether the current kernel and process capabilities support it.
func probeEBPF() error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("remove memlock: %w", err)
	}

	spec, err := portbpf.LoadPortMonitor()
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	var objs struct {
		Prog *ebpf.Program `ebpf:"trace_inet_sock_set_state"`
	}
	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		return fmt.Errorf("load program: %w", err)
	}
	objs.Prog.Close()
	return nil
}

func newEBPFMonitor(logger *slog.Logger) *ebpfMonitor {
	return &ebpfMonitor{
		events: make(chan PortEvent, 50),
		logger: logger,
	}
}

func (m *ebpfMonitor) Start(ctx context.Context) error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("remove memlock rlimit: %w", err)
	}

	var objs portbpf.PortMonitorObjects
	if err := portbpf.LoadPortMonitorObjects(&objs, nil); err != nil {
		return fmt.Errorf("load eBPF objects: %w", err)
	}

	tp, err := link.Tracepoint("sock", "inet_sock_set_state", objs.TraceInetSockSetState, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attach tracepoint: %w", err)
	}

	reader, err := perf.NewReader(objs.Events, 4096)
	if err != nil {
		tp.Close()
		objs.Close()
		return fmt.Errorf("create perf reader: %w", err)
	}

	// Capture initial listening ports so consumers see the same PortOpened
	// burst they'd get from the polling monitor on startup.
	initialPorts, err := GetListeningPorts()
	if err != nil {
		m.logger.Warn("failed to read initial ports for eBPF monitor", "error", err)
	}
	for _, p := range initialPorts {
		m.events <- PortEvent{
			Type:      PortOpened,
			Port:      p.Port,
			Protocol:  p.Protocol,
			Timestamp: time.Now(),
		}
	}

	go m.readLoop(ctx, reader, tp, &objs)
	return nil
}

func (m *ebpfMonitor) Events() <-chan PortEvent {
	return m.events
}

func (m *ebpfMonitor) readLoop(ctx context.Context, reader *perf.Reader, tp link.Link, objs *portbpf.PortMonitorObjects) {
	defer close(m.events)
	defer reader.Close()
	defer tp.Close()
	defer objs.Close()

	go func() {
		<-ctx.Done()
		reader.Close()
	}()

	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return
			}
			m.logger.Debug("perf read error", "error", err)
			continue
		}

		if record.LostSamples > 0 {
			m.logger.Warn("lost eBPF samples", "count", record.LostSamples)
			continue
		}

		raw := record.RawSample
		// port_event is 16 bytes: u32 pid, u16 sport, u16 family, s32 old_state, s32 new_state
		if len(raw) < 16 {
			m.logger.Debug("eBPF event too short", "len", len(raw))
			continue
		}

		pid := binary.LittleEndian.Uint32(raw[0:4])
		sport := binary.LittleEndian.Uint16(raw[4:6])
		family := binary.LittleEndian.Uint16(raw[6:8])
		oldState := int32(binary.LittleEndian.Uint32(raw[8:12]))
		newState := int32(binary.LittleEndian.Uint32(raw[12:16]))

		var evtType EventType
		protocol := "tcp"
		if family == 10 { // AF_INET6
			protocol = "tcp6"
		}

		switch {
		case newState == tcpListen:
			evtType = PortOpened
		case oldState == tcpListen:
			evtType = PortClosed
		default:
			continue
		}

		pe := PortEvent{
			Type:      evtType,
			PID:       int(pid),
			Port:      int(sport),
			Protocol:  protocol,
			Timestamp: time.Now(),
		}

		select {
		case m.events <- pe:
			m.logger.Debug("eBPF port event",
				"type", pe.Type,
				"port", pe.Port,
				"pid", pe.PID,
				"protocol", pe.Protocol)
		default:
			m.logger.Warn("event channel full, dropping eBPF event")
		}
	}
}
