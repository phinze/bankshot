package protocol

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Request
		wantErr bool
	}{
		{
			name: "valid open request",
			input: `{
				"id": "test-123",
				"type": "open",
				"payload": {"url": "https://example.com"}
			}`,
			want: &Request{
				ID:      "test-123",
				Type:    CommandOpen,
				Payload: json.RawMessage(`{"url": "https://example.com"}`),
			},
			wantErr: false,
		},
		{
			name: "valid forward request",
			input: `{
				"id": "test-456",
				"type": "forward",
				"payload": {"remote_port": 8080, "local_port": 8081, "connection_info": "user@host"}
			}`,
			want: &Request{
				ID:      "test-456",
				Type:    CommandForward,
				Payload: json.RawMessage(`{"remote_port": 8080, "local_port": 8081, "connection_info": "user@host"}`),
			},
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid json}`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   ``,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRequest([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.ID != tt.want.ID {
					t.Errorf("ParseRequest() ID = %v, want %v", got.ID, tt.want.ID)
				}
				if got.Type != tt.want.Type {
					t.Errorf("ParseRequest() Type = %v, want %v", got.Type, tt.want.Type)
				}
			}
		})
	}
}

func TestMarshalResponse(t *testing.T) {
	tests := []struct {
		name    string
		resp    *Response
		wantErr bool
	}{
		{
			name: "success response with data",
			resp: &Response{
				ID:      "test-123",
				Success: true,
				Data:    json.RawMessage(`{"message": "hello"}`),
			},
			wantErr: false,
		},
		{
			name: "error response",
			resp: &Response{
				ID:      "test-456",
				Success: false,
				Error:   "something went wrong",
			},
			wantErr: false,
		},
		{
			name: "minimal response",
			resp: &Response{
				ID:      "test-789",
				Success: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalResponse(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				var parsed Response
				if err := json.Unmarshal(got, &parsed); err != nil {
					t.Errorf("MarshalResponse() returned invalid JSON: %v", err)
				}
				if parsed.ID != tt.resp.ID {
					t.Errorf("MarshalResponse() ID = %v, want %v", parsed.ID, tt.resp.ID)
				}
				if parsed.Success != tt.resp.Success {
					t.Errorf("MarshalResponse() Success = %v, want %v", parsed.Success, tt.resp.Success)
				}
			}
		})
	}
}

func TestNewErrorResponse(t *testing.T) {
	testErr := errors.New("test error")
	resp := NewErrorResponse("test-id", testErr)

	if resp.ID != "test-id" {
		t.Errorf("NewErrorResponse() ID = %v, want %v", resp.ID, "test-id")
	}
	if resp.Success != false {
		t.Errorf("NewErrorResponse() Success = %v, want %v", resp.Success, false)
	}
	if resp.Error != "test error" {
		t.Errorf("NewErrorResponse() Error = %v, want %v", resp.Error, "test error")
	}
}

func TestNewSuccessResponse(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		data    interface{}
		wantErr bool
	}{
		{
			name: "string data",
			id:   "test-1",
			data: "success",
			wantErr: false,
		},
		{
			name: "struct data",
			id:   "test-2",
			data: StatusResponse{
				Version:        "1.0.0",
				Uptime:         "1h",
				ActiveForwards: 2,
			},
			wantErr: false,
		},
		{
			name: "nil data",
			id:   "test-3",
			data: nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := NewSuccessResponse(tt.id, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSuccessResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if resp.ID != tt.id {
					t.Errorf("NewSuccessResponse() ID = %v, want %v", resp.ID, tt.id)
				}
				if resp.Success != true {
					t.Errorf("NewSuccessResponse() Success = %v, want %v", resp.Success, true)
				}
			}
		})
	}
}

func TestPayloadParsing(t *testing.T) {
	t.Run("OpenRequest", func(t *testing.T) {
		req := Request{
			ID:      "test",
			Type:    CommandOpen,
			Payload: json.RawMessage(`{"url": "https://example.com"}`),
		}

		var openReq OpenRequest
		if err := json.Unmarshal(req.Payload, &openReq); err != nil {
			t.Errorf("Failed to unmarshal OpenRequest: %v", err)
		}
		if openReq.URL != "https://example.com" {
			t.Errorf("OpenRequest URL = %v, want %v", openReq.URL, "https://example.com")
		}
	})

	t.Run("ForwardRequest", func(t *testing.T) {
		req := Request{
			ID:      "test",
			Type:    CommandForward,
			Payload: json.RawMessage(`{
				"remote_port": 8080,
				"local_port": 8081,
				"host": "127.0.0.1",
				"connection_info": "user@host",
				"socket_path": "/tmp/socket"
			}`),
		}

		var forwardReq ForwardRequest
		if err := json.Unmarshal(req.Payload, &forwardReq); err != nil {
			t.Errorf("Failed to unmarshal ForwardRequest: %v", err)
		}
		if forwardReq.RemotePort != 8080 {
			t.Errorf("ForwardRequest RemotePort = %v, want %v", forwardReq.RemotePort, 8080)
		}
		if forwardReq.LocalPort != 8081 {
			t.Errorf("ForwardRequest LocalPort = %v, want %v", forwardReq.LocalPort, 8081)
		}
		if forwardReq.Host != "127.0.0.1" {
			t.Errorf("ForwardRequest Host = %v, want %v", forwardReq.Host, "127.0.0.1")
		}
		if forwardReq.ConnectionInfo != "user@host" {
			t.Errorf("ForwardRequest ConnectionInfo = %v, want %v", forwardReq.ConnectionInfo, "user@host")
		}
	})

	t.Run("UnforwardRequest", func(t *testing.T) {
		req := Request{
			ID:      "test",
			Type:    CommandUnforward,
			Payload: json.RawMessage(`{
				"remote_port": 8080,
				"host": "localhost",
				"connection_info": "user@host"
			}`),
		}

		var unforwardReq UnforwardRequest
		if err := json.Unmarshal(req.Payload, &unforwardReq); err != nil {
			t.Errorf("Failed to unmarshal UnforwardRequest: %v", err)
		}
		if unforwardReq.RemotePort != 8080 {
			t.Errorf("UnforwardRequest RemotePort = %v, want %v", unforwardReq.RemotePort, 8080)
		}
		if unforwardReq.Host != "localhost" {
			t.Errorf("UnforwardRequest Host = %v, want %v", unforwardReq.Host, "localhost")
		}
		if unforwardReq.ConnectionInfo != "user@host" {
			t.Errorf("UnforwardRequest ConnectionInfo = %v, want %v", unforwardReq.ConnectionInfo, "user@host")
		}
	})
}