package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"qq-pet/backend/internal/automation"
	"qq-pet/backend/internal/petapi"
	"strconv"
	"time"
)

func New(engine *automation.Engine, version string, connected func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"version": version, "connected": connected(), "api_count": len(petapi.Specs), "framework_min": "2.6.0", "contract_date": "2026-10-14", "actions": petapi.Specs})
	})
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		bots, err := engine.Bots(ctx)
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, bots)
	})
	mux.HandleFunc("GET /api/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(w, r)
		if !ok {
			return
		}
		a, err := engine.Store.Account(r.Context(), id)
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, a)
	})
	mux.HandleFunc("PUT /api/accounts/{id}/config", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(w, r)
		if !ok {
			return
		}
		var input struct {
			Config   automation.Config `json:"config"`
			Revision int               `json:"revision"`
		}
		if !body(w, r, &input) {
			return
		}
		if err := engine.Configure(r.Context(), id, input.Config, input.Revision); err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, map[string]bool{"saved": true})
	})
	mux.HandleFunc("GET /api/accounts/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(w, r)
		if !ok {
			return
		}
		logs, err := engine.Store.Logs(r.Context(), id)
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, logs)
	})
	mux.HandleFunc("POST /api/accounts/{id}/action", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(w, r)
		if !ok {
			return
		}
		var input struct {
			Action string         `json:"action"`
			Params map[string]any `json:"params"`
		}
		if !body(w, r, &input) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		data, err := engine.Action(ctx, id, input.Action, input.Params)
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, data)
	})
	mux.HandleFunc("POST /api/accounts/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if err := engine.Tick(ctx, id); err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, map[string]bool{"checked": true})
	})
	mux.HandleFunc("POST /api/accounts/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(w, r)
		if !ok {
			return
		}
		var input struct {
			Confirmed bool   `json:"confirmed"`
			StoryID   string `json:"story_id"`
			ClearTask bool   `json:"clear_task"`
		}
		if !body(w, r, &input) {
			return
		}
		if !input.Confirmed {
			http.Error(w, "需要先确认已核对官方状态", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := engine.Resolve(ctx, id, input.StoryID, input.ClearTask); err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, map[string]bool{"resolved": true})
	})
	mux.Handle("/", frontendHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
func accountID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id <= 0 {
		http.Error(w, "无效 QQ 账号", 400)
		return 0, false
	}
	return id, true
}
func body(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "请求格式错误："+err.Error(), 400)
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "请求只能包含一个 JSON 对象", 400)
		return false
	}
	return true
}
func reply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func failure(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var code any = 0
	var rpc *petapi.RPCError
	if errors.As(err, &rpc) {
		code = rpc.Code
	}
	if errors.Is(err, automation.ErrRevision) {
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "code": code})
}
