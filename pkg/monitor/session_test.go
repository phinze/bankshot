package monitor

import "testing"

func TestShouldForwardPort(t *testing.T) {
	tests := []struct {
		name        string
		port        int
		portRanges  []PortRange
		ignorePorts map[int]bool
		want        bool
	}{
		{
			name: "default non-privileged port",
			port: 8080, portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "privileged port rejected",
			port: 22, portRanges: nil, ignorePorts: nil,
			want: false,
		},
		{
			name: "boundary 1024 accepted",
			port: 1024, portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "boundary 1023 rejected",
			port: 1023, portRanges: nil, ignorePorts: nil,
			want: false,
		},
		{
			name: "high ephemeral port (OAuth)",
			port: 37593, portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "ignored port rejected",
			port: 8080, portRanges: nil, ignorePorts: map[int]bool{8080: true},
			want: false,
		},
		{
			name: "explicit range includes port",
			port: 5000, portRanges: []PortRange{{3000, 9999}}, ignorePorts: nil,
			want: true,
		},
		{
			name: "explicit range excludes port",
			port: 37593, portRanges: []PortRange{{3000, 9999}}, ignorePorts: nil,
			want: false,
		},
		{
			name: "ignore beats explicit range",
			port: 5000, portRanges: []PortRange{{3000, 9999}}, ignorePorts: map[int]bool{5000: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldForwardPort(tt.port, tt.portRanges, tt.ignorePorts)
			if got != tt.want {
				t.Errorf("ShouldForwardPort(%d, %v, %v) = %v, want %v",
					tt.port, tt.portRanges, tt.ignorePorts, got, tt.want)
			}
		})
	}
}
