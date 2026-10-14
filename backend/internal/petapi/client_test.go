package petapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestReconnectDoesNotReplayWrites(t *testing.T) {
	var connections, writes atomic.Int32
	up := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		var auth map[string]any
		if ws.ReadJSON(&auth) != nil || auth["token"] != "test-token" {
			return
		}
		connections.Add(1)
		_ = ws.WriteJSON(map[string]any{"type": "auth_ok"})
		for {
			var req map[string]any
			if ws.ReadJSON(&req) != nil {
				return
			}
			if req["action"] == "buy_pet_food" {
				writes.Add(1)
				return
			}
			if req["type"] == "action" {
				_ = ws.WriteJSON(map[string]any{"type": "action_result", "id": req["id"], "ok": true, "data": map[string]any{"pet_id": "real"}})
			}
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	client := New(u.Hostname(), port, "test-token", "2.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { client.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	wait := func(test func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !test() {
			if time.Now().After(deadline) {
				t.Fatal("timeout waiting for connection")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait(client.Connected)
	call, cancelCall := context.WithTimeout(ctx, time.Second)
	defer cancelCall()
	_, err := client.Call(call, "buy_pet_food", map[string]any{"count": 1})
	var unknown *UnknownOutcome
	if !errors.As(err, &unknown) {
		t.Fatalf("expected uncertain write, got %v", err)
	}
	wait(func() bool { return connections.Load() >= 2 && client.Connected() })
	if writes.Load() != 1 {
		t.Fatal("write replayed on reconnect")
	}
	data, err := client.Call(call, "get_pet_profile", nil)
	if errors.Is(err, context.DeadlineExceeded) {
		call2, cancel2 := context.WithTimeout(ctx, time.Second)
		defer cancel2()
		data, err = client.Call(call2, "get_pet_profile", nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(data, &result) != nil || result["pet_id"] != "real" {
		t.Fatal(string(data))
	}
}
func TestContractValidation(t *testing.T) {
	if len(Specs) != 30 {
		t.Fatal("expected 30 official pet actions")
	}
	for _, s := range Specs {
		if s.URL == "" || len(s.Fields) < 2 {
			t.Fatal(s.Action)
		}
	}
	for _, test := range []struct {
		action string
		params map[string]any
	}{
		{"send_packet", nil}, {"get_pet_vitals", map[string]any{}}, {"feed_pet", map[string]any{"pet_id": "real", "feed_type": 123}}, {"settle_pet_pk", map[string]any{"pet_id": "real", "story_id": "6400_wrong"}}, {"start_pet_activity", map[string]any{"pet_id": "real", "activity": "school", "option_name": "course", "sub_event_type": 0}}, {"get_pet_profile", map[string]any{"invented": 1}},
	} {
		if _, err := Prepare(123, test.action, test.params); err == nil {
			t.Fatalf("invalid params accepted: %s", test.action)
		}
	}
	p, err := Prepare(123, "get_pet_vitals", map[string]any{"self_id": 999, "client_type": "linuxqq", "pet_id": "real"})
	if err != nil || p["self_id"] != int64(123) || p["client_type"] != "android" {
		t.Fatal("account routing not pinned")
	}
}

func TestFrameworkStringErrorCode(t *testing.T) {
	up := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		var request map[string]any
		if ws.ReadJSON(&request) != nil {
			return
		}
		_ = ws.WriteJSON(map[string]any{"type": "auth_ok"})
		for {
			if ws.ReadJSON(&request) != nil {
				return
			}
			_ = ws.WriteJSON(map[string]any{"type": "action_result", "id": request["id"], "ok": false, "error_code": "ACCOUNT_OFFLINE", "error": "offline"})
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	c := New(u.Hostname(), port, "test-token", "2.0.0")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	for !c.Connected() {
		if ctx.Err() != nil {
			t.Fatal("no connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := c.Call(ctx, "get_pet_profile", nil)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != "ACCOUNT_OFFLINE" || !c.Connected() {
		t.Fatalf("string code broke connection: %v", err)
	}
}
