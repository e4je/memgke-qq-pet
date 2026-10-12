// Package managed implements Mengka's websocket-v1 managed runtime contract.
package managed

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/response"
)

type Runtime struct {
	InstanceID string
	Origin     string
	adminToken string
	mu         sync.RWMutex
	active     bool
	closed     bool
	stop       func()
}

func readFile(name string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(name) {
		return nil, errors.New("managed file path must be absolute")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("managed file is too large")
	}
	return b, nil
}

func readToken(name, connectionFile string) (string, error) {
	if !filepath.IsAbs(name) || filepath.Dir(filepath.Clean(name)) != filepath.Dir(filepath.Clean(connectionFile)) {
		return "", errors.New("managed token must be in the private connection directory")
	}
	realName, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", err
	}
	realConnection, err := filepath.EvalSymlinks(connectionFile)
	if err != nil {
		return "", err
	}
	if filepath.Dir(realName) != filepath.Dir(realConnection) {
		return "", errors.New("managed token escapes private directory")
	}
	b, err := readFile(name, 4096)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if len(token) < 24 {
		return "", errors.New("invalid managed token")
	}
	return token, nil
}

// Load applies framework-owned settings before opening the database. It never
// falls back to standalone mode when a partial managed environment is present.
func Load(cfg *config.Config) (*Runtime, error) {
	if os.Getenv("MENGKA_MANAGED_V1") != "1" {
		for _, entry := range os.Environ() {
			key, value, _ := strings.Cut(entry, "=")
			if value != "" && (key == "MENGKA_MANAGED_V1" || strings.HasPrefix(key, "MENGKA_PLUGIN_")) {
				return nil, errors.New("incomplete managed environment")
			}
		}
		return nil, nil
	}
	file := os.Getenv("MENGKA_PLUGIN_CONNECTION_FILE")
	b, err := readFile(file, 98304)
	if err != nil {
		return nil, err
	}
	var c struct {
		Schema        int    `json:"schema"`
		Transport     string `json:"transport"`
		InstanceID    string `json:"instance_id"`
		WebsocketURL  string `json:"websocket_url"`
		TokenFile     string `json:"token_file"`
		DataDirectory string `json:"data_directory"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, errors.New("invalid managed connection JSON")
	}
	if c.Schema != 1 || c.Transport != "websocket-v1" || !regexp.MustCompile(`^[a-zA-Z0-9._-]+$`).MatchString(c.InstanceID) {
		return nil, errors.New("unsupported managed connection")
	}
	u, err := url.Parse(c.WebsocketURL)
	if err != nil || u.Scheme != "ws" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("managed WebSocket must use loopback")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid managed WebSocket port")
	}
	token, err := readToken(c.TokenFile, file)
	if err != nil {
		return nil, err
	}
	dataDir := os.Getenv("MENGKA_PLUGIN_DATA_DIR")
	if !filepath.IsAbs(dataDir) || !filepath.IsAbs(c.DataDirectory) {
		return nil, errors.New("invalid managed data directory")
	}
	dataDir, err = filepath.EvalSymlinks(dataDir)
	if err != nil {
		return nil, err
	}
	connectionData, err := filepath.EvalSymlinks(c.DataDirectory)
	if err != nil || dataDir != connectionData {
		return nil, errors.New("managed data directory mismatch")
	}
	info, err := os.Stat(dataDir)
	if err != nil || !info.IsDir() {
		return nil, errors.New("managed data path is not a directory")
	}
	adminToken, err := readToken(os.Getenv("MENGKA_PLUGIN_ADMIN_TOKEN_FILE"), file)
	if err != nil {
		return nil, err
	}
	adminPort, err := strconv.Atoi(os.Getenv("MENGKA_PLUGIN_ADMIN_PORT"))
	if err != nil || adminPort < 1 || adminPort > 65535 || os.Getenv("MENGKA_PLUGIN_ADMIN_HOST") != "127.0.0.1" {
		return nil, errors.New("invalid managed admin address")
	}
	origin, err := url.Parse(os.Getenv("MENGKA_PLUGIN_ADMIN_ORIGIN"))
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.User != nil || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
		return nil, errors.New("invalid managed admin origin")
	}
	cfg.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(adminPort))
	cfg.DatabasePath = filepath.Join(dataDir, "qq-pet.db")
	cfg.FrontendOrigin = origin.Scheme + "://" + origin.Host
	cfg.CookieSecure = origin.Scheme == "https"
	cfg.ManagedConnection = &config.ManagedConnection{Host: u.Hostname(), Port: port, Token: token}
	return &Runtime{InstanceID: c.InstanceID, Origin: cfg.FrontendOrigin, adminToken: adminToken}, nil
}

func (rt *Runtime) authorized(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Mengka-Managed-Token")), []byte(rt.adminToken)) != 1 {
		return false
	}
	control := r.URL.Path == "/api/managed/health" || r.URL.Path == "/api/managed/control"
	if !control && r.Header.Get("X-Mengka-Managed-Origin") != rt.Origin {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != rt.Origin {
		return false
	}
	return true
}

// Wrap authenticates the gateway before providing SSO and waits for activate
// before admitting business requests or starting scheduled work.
func (rt *Runtime) Wrap(next http.Handler, user auth.User, connected func() bool, start, stop func()) http.Handler {
	rt.stop = stop
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rt.authorized(r) {
			http.Error(w, "unauthorized managed gateway", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/managed/health" && r.Method == http.MethodGet {
			online := connected()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"ready": online, "connected": online, "configured": true})
			return
		}
		if r.URL.Path == "/api/managed/control" && r.Method == http.MethodPost {
			var input struct {
				Action string `json:"action"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || (input.Action != "activate" && input.Action != "stop") {
				http.Error(w, "invalid control action", http.StatusBadRequest)
				return
			}
			rt.mu.Lock()
			defer rt.mu.Unlock()
			if rt.closed {
				http.Error(w, "plugin is shutting down", http.StatusServiceUnavailable)
				return
			}
			active := input.Action == "activate"
			if active != rt.active {
				if active {
					start()
				} else {
					stop()
				}
				rt.active = active
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"active": rt.active})
			return
		}
		rt.mu.RLock()
		active := rt.active && !rt.closed
		if !active {
			rt.mu.RUnlock()
			http.Error(w, "plugin is not active", http.StatusServiceUnavailable)
			return
		}
		// Drain ordinary business requests before stop, but do not let the
		// long-lived, read-only log WebSocket block lifecycle commands.
		if strings.HasPrefix(r.URL.Path, "/ws/") {
			rt.mu.RUnlock()
		} else {
			defer rt.mu.RUnlock()
		}
		// The connection is supplied by the framework. Do not let the manual
		// configuration screen replace it or persist an ephemeral token.
		if r.URL.Path == "/api/admin/mokant/config" && r.Method == http.MethodGet {
			response.JSON(w, 200, 0, "连接由萌卡框架托管", map[string]any{"managed": true, "mokant_token_set": true})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/admin/mokant/") && r.Method != http.MethodGet && r.URL.Path != "/api/admin/mokant/debug" {
			response.JSON(w, 409, 4004, "连接由萌卡框架托管，请在框架中管理插件", nil)
			return
		}
		next.ServeHTTP(w, auth.WithManagedAdmin(r, user))
	})
}

func (rt *Runtime) Close() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.closed = true
	if rt.active && rt.stop != nil {
		rt.stop()
	}
	rt.active = false
}

func (rt *Runtime) String() string { return fmt.Sprintf("managed instance %s", rt.InstanceID) }
