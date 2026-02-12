package monitor

// Compile-time checks that our monitors satisfy PortEventSource.
var (
	_ PortEventSource = (*Monitor)(nil)
	_ PortEventSource = (*SystemMonitor)(nil)
)
