package monitor

// Compile-time check that the polling monitor satisfies PortEventSource.
var _ PortEventSource = (*Monitor)(nil)
