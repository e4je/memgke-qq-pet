package managed

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qq-pet/backend/internal/config"
)

func fixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	private, data := t.TempDir(), t.TempDir()
	file := filepath.Join(private, "connection.json")
	token := filepath.Join(private, "token")
	if err := os.WriteFile(token, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"MENGKA_MANAGED_V1": "1", "MENGKA_PLUGIN_CONNECTION_FILE": file, "MENGKA_PLUGIN_DATA_DIR": data, "MENGKA_PLUGIN_ADMIN_TOKEN_FILE": token, "MENGKA_PLUGIN_ADMIN_HOST": "127.0.0.1", "MENGKA_PLUGIN_ADMIN_PORT": "12345", "MENGKA_PLUGIN_ADMIN_ORIGIN": "https://pet.example"} {
		t.Setenv(key, value)
	}
	c := map[string]any{"schema": 1, "transport": "websocket-v1", "instance_id": "pet-1", "websocket_url": "ws://127.0.0.1:12346/", "token_file": token, "data_directory": data}
	writeConnection(t, file, c)
	return file, c
}

func writeConnection(t *testing.T, file string, c map[string]any) {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsInvalidManagedRuntime(t *testing.T) {
	for _, test := range []struct {
		name, key string
		value     any
	}{
		{"remote websocket", "websocket_url", "ws://example.com:3001/"},
		{"query token", "websocket_url", "ws://127.0.0.1:3001/?token=secret"},
		{"relative token", "token_file", "token"},
		{"wrong transport", "transport", "native-ipc-v1"},
		{"relative data", "data_directory", "./data"},
		{"wrong schema", "schema", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, c := fixture(t)
			c[test.key] = test.value
			writeConnection(t, file, c)
			if _, err := Load(&config.Config{}); err == nil {
				t.Fatal("invalid runtime accepted")
			}
		})
	}
}

func TestManagedOverridesManualSettings(t *testing.T) {
	fixture(t)
	cfg := config.Config{Addr: ":9999", DatabasePath: "./wrong.db"}
	rt, err := Load(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:12345" || cfg.DatabasePath != filepath.Join(os.Getenv("MENGKA_PLUGIN_DATA_DIR"), "qq-pet.db") || cfg.ManagedConnection.Port != 12346 || rt.Origin != "https://pet.example" {
		t.Fatal("framework settings not applied")
	}
	// No gateway credential or a wrong peer/origin can provide SSO.
	for _, test := range []struct {
		peer, token, managedOrigin, origin string
		want                               bool
	}{
		{"127.0.0.1:10", rt.adminToken, rt.Origin, "", true},
		{"127.0.0.1:10", "", rt.Origin, "", false},
		{"192.0.2.1:10", rt.adminToken, rt.Origin, "", false},
		{"127.0.0.1:10", rt.adminToken, "", "", false},
		{"127.0.0.1:10", rt.adminToken, rt.Origin, "https://evil.example", false},
	} {
		r := httptest.NewRequest("GET", "/api/auth/session", nil)
		r.RemoteAddr = test.peer
		r.Header.Set("X-Mengka-Managed-Token", test.token)
		r.Header.Set("X-Mengka-Managed-Origin", test.managedOrigin)
		r.Header.Set("Origin", test.origin)
		if rt.authorized(r) != test.want {
			t.Fatal("gateway authorization mismatch")
		}
	}
	t.Setenv("MENGKA_MANAGED_V1", "")
	if _, err := Load(&config.Config{}); err == nil {
		t.Fatal("partial runtime silently accepted")
	}
}
