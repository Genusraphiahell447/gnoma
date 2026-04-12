package mcp

import (
	"encoding/json"
	"testing"
)

func TestRequest_MarshalRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		req  Request
	}{
		{
			name: "with params",
			req: Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "initialize",
				Params:  json.RawMessage(`{"capabilities":{}}`),
			},
		},
		{
			name: "nil params omitted",
			req: Request{
				JSONRPC: "2.0",
				ID:      42,
				Method:  "tools/list",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var got Request
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got.JSONRPC != tt.req.JSONRPC {
				t.Errorf("JSONRPC = %q, want %q", got.JSONRPC, tt.req.JSONRPC)
			}
			if got.ID != tt.req.ID {
				t.Errorf("ID = %d, want %d", got.ID, tt.req.ID)
			}
			if got.Method != tt.req.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.req.Method)
			}
			if string(got.Params) != string(tt.req.Params) {
				t.Errorf("Params = %s, want %s", got.Params, tt.req.Params)
			}
		})
	}
}

func TestResponse_MarshalRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		resp Response
	}{
		{
			name: "success result",
			resp: Response{
				JSONRPC: "2.0",
				ID:      1,
				Result:  json.RawMessage(`{"serverInfo":{"name":"test"}}`),
			},
		},
		{
			name: "error response",
			resp: Response{
				JSONRPC: "2.0",
				ID:      2,
				Error: &RPCError{
					Code:    -32601,
					Message: "method not found",
				},
			},
		},
		{
			name: "error with data",
			resp: Response{
				JSONRPC: "2.0",
				ID:      3,
				Error: &RPCError{
					Code:    -32000,
					Message: "server error",
					Data:    json.RawMessage(`{"detail":"something broke"}`),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var got Response
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got.JSONRPC != tt.resp.JSONRPC {
				t.Errorf("JSONRPC = %q, want %q", got.JSONRPC, tt.resp.JSONRPC)
			}
			if got.ID != tt.resp.ID {
				t.Errorf("ID = %d, want %d", got.ID, tt.resp.ID)
			}
			if string(got.Result) != string(tt.resp.Result) {
				t.Errorf("Result = %s, want %s", got.Result, tt.resp.Result)
			}
			if (got.Error == nil) != (tt.resp.Error == nil) {
				t.Fatalf("Error nil mismatch: got %v, want %v", got.Error, tt.resp.Error)
			}
			if got.Error != nil {
				if got.Error.Code != tt.resp.Error.Code {
					t.Errorf("Error.Code = %d, want %d", got.Error.Code, tt.resp.Error.Code)
				}
				if got.Error.Message != tt.resp.Error.Message {
					t.Errorf("Error.Message = %q, want %q", got.Error.Message, tt.resp.Error.Message)
				}
			}
		})
	}
}

func TestNotification_OmitsID(t *testing.T) {
	n := Notification{
		JSONRPC: "2.0",
		Method:  "initialized",
	}

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Notification must not have an "id" field.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["id"]; ok {
		t.Error("notification should not contain 'id' field")
	}
}

func TestRPCError_Error(t *testing.T) {
	e := &RPCError{Code: -32601, Message: "method not found"}
	got := e.Error()
	want := "rpc error -32601: method not found"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestRequest_NilParams_MarshalOmitsField(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
		Params:  nil,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["params"]; ok {
		t.Error("nil Params should be omitted from JSON")
	}
}
