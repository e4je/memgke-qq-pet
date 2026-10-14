package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"qq-pet/backend/internal/automation"
)

func TestManagedServerProcess(t *testing.T) {
	if os.Getenv("QQ_PET_TEST_SERVER") != "1" {
		return
	}
	main()
}

// Exercises the real entrypoint, HTTP router, SQLite migrations, WebSocket
// authentication and process restart. No QQ account or external service is used.
func TestManagedStartupAndRestart(t *testing.T) {
	data := t.TempDir()
	private := t.TempDir()
	connectionFile := filepath.Join(private, "connection.json")
	tokenFile := filepath.Join(private, "ws.token")
	adminFile := filepath.Join(private, "admin.token")
	adminToken := strings.Repeat("a", 32)
	if err := os.WriteFile(adminFile, []byte(adminToken), 0600); err != nil {
		t.Fatal(err)
	}

	for cycle := 0; cycle < 2; cycle++ {
		token := strings.Repeat(fmt.Sprint(cycle+1), 32)
		if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		upgrader := websocket.Upgrader{}
		framework := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			var auth map[string]any
			if ws.ReadJSON(&auth) != nil || auth["type"] != "auth" || auth["token"] != token {
				return
			}
			_ = ws.WriteJSON(map[string]any{"type": "auth_ok"})
			for {
				var req map[string]any
				if ws.ReadJSON(&req) != nil {
					return
				}
				if req["type"] == "action" {
					_ = ws.WriteJSON(map[string]any{"type": "action_result", "id": req["id"], "ok": true, "data": []any{}})
				}
			}
		}))
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		metadata, _ := json.Marshal(map[string]any{"schema": 1, "transport": "websocket-v1", "instance_id": "pet-test", "websocket_url": "ws" + strings.TrimPrefix(framework.URL, "http"), "token_file": tokenFile, "data_directory": data, "allowed_directories": []string{}})
		if err := os.WriteFile(connectionFile, metadata, 0600); err != nil {
			t.Fatal(err)
		}
		origin := "https://pet.example.test"
		cmd := exec.Command(os.Args[0], "-test.run=^TestManagedServerProcess$")
		if binary := os.Getenv("QQ_PET_TEST_BINARY"); binary != "" {
			cmd = exec.Command(binary)
		}
		cmd.Dir = t.TempDir() // immutable program directory must not receive data
		for _, env := range os.Environ() {
			key, _, _ := strings.Cut(env, "=")
			if !strings.HasPrefix(key, "MENGKA_") && !strings.HasPrefix(key, "QQ_PET_TEST_") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		cmd.Env = append(cmd.Env, "QQ_PET_TEST_SERVER=1", "MENGKA_MANAGED_V1=1", "MENGKA_PLUGIN_CONNECTION_FILE="+connectionFile, "MENGKA_PLUGIN_DATA_DIR="+data, "MENGKA_PLUGIN_ADMIN_HOST=127.0.0.1", fmt.Sprintf("MENGKA_PLUGIN_ADMIN_PORT=%d", port), "MENGKA_PLUGIN_ADMIN_TOKEN_FILE="+adminFile, "MENGKA_PLUGIN_ADMIN_ORIGIN="+origin)
		logFile, err := os.Create(filepath.Join(t.TempDir(), "process.log"))
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdout, cmd.Stderr = logFile, logFile
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cleaned := false
		cleanup := func() {
			if !cleaned {
				cleaned = true
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				logFile.Close()
				framework.Close()
			}
		}
		t.Cleanup(cleanup)
		client := &http.Client{Timeout: 2 * time.Second}
		call := func(path, body string, authorized bool, browserOrigin string) (int, []byte, error) {
			method := "GET"
			if body != "" {
				method = "POST"
			}
			req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), strings.NewReader(body))
			if authorized {
				req.Header.Set("X-Mengka-Managed-Token", adminToken)
				req.Header.Set("X-Mengka-Managed-Origin", origin)
			}
			if browserOrigin != "" {
				req.Header.Set("Origin", browserOrigin)
			}
			res, err := client.Do(req)
			if err != nil {
				return 0, nil, err
			}
			defer res.Body.Close()
			b, err := io.ReadAll(res.Body)
			return res.StatusCode, b, err
		}
		ready := false
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			status, b, err := call("/api/managed/health", "", true, "")
			if err == nil && status == 200 && bytes.Contains(b, []byte(`"ready":true`)) {
				ready = true
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if !ready {
			logs, _ := os.ReadFile(logFile.Name())
			t.Fatalf("managed startup failed: %s", logs)
		}
		check := func(path, body string, authorized bool, origin string, want int) []byte {
			t.Helper()
			status, b, err := call(path, body, authorized, origin)
			if err != nil || status != want {
				t.Fatalf("%s: status=%d want=%d err=%v body=%s", path, status, want, err, b)
			}
			return b
		}
		check("/api/info", "", false, "", 401)
		check("/api/info", "", true, "", 503)
		check("/api/managed/control", `{"action":"activate"}`, true, "", 200)
		check("/api/managed/control", `{"action":"activate"}`, true, "", 200)
		check("/api/info", "", true, "https://evil.example", 401)
		var session struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(check("/api/info", "", true, "", 200), &session); err != nil {
			t.Fatal(err)
		}
		expectedVersion := os.Getenv("QQ_PET_TEST_EXPECTED_VERSION")
		if expectedVersion == "" {
			expectedVersion = "2.0.1"
		}
		if session.Data["version"] != expectedVersion || session.Data["api_count"] != float64(30) {
			t.Fatalf("invalid info: %v", session)
		}
		check("/api/accounts", "", true, "", 200)
		check("/api/accounts/123", "", true, "", 200)
		check("/api/accounts/invalid", "", true, "", 400)
		check("/api/accounts/123/resolve", `{"confirmed":false}`, true, "", 400)
		check("/api/accounts/123/action", `{"action":"send_packet","params":{}}`, true, "", 502)
		if b := check("/admin", "", true, "", 200); !bytes.Contains(b, []byte("<html")) {
			t.Fatal("Vue page is missing")
		}
		check("/api/missing", "", true, "", 404)
		check("/assets/missing.js", "", true, "", 404)
		check("/api/auth/session", "", true, "", 404)
		check("/api/managed/control", `{"action":"stop"}`, true, "", 200)
		check("/api/info", "", true, "", 503)
		check("/api/managed/control", `{"action":"activate"}`, true, "", 200)
		check("/api/info", "", true, "", 200)
		cleanup()
		if entries, _ := os.ReadDir(cmd.Dir); len(entries) != 0 {
			t.Fatal("managed server wrote into program directory")
		}
		store, err := automation.Open(filepath.Join(data, "qq-pet.db"))
		if err != nil {
			t.Fatal(err)
		}
		if cycle == 0 {
			config := automation.DefaultConfig()
			config.Start = "09:30"
			if err = store.Configure(context.Background(), 123, config, 0); err != nil {
				t.Fatal(err)
			}
		} else {
			account, err := store.Account(context.Background(), 123)
			if err != nil || account.Config.Start != "09:30" {
				t.Fatal("settings lost across process restart", err)
			}
		}
		store.Close()
		content, err := os.ReadFile(filepath.Join(data, "qq-pet.db"))
		if err != nil || bytes.Contains(content, []byte(token)) || bytes.Contains(content, []byte(adminToken)) {
			t.Fatal("managed credentials persisted")
		}
	}
}
