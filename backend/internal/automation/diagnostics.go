package automation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"qq-pet/backend/internal/petapi"
	"strconv"
	"strings"
	"time"
)

type traceKey struct{}
type trace struct {
	ID      string
	Source  string
	Account int64
}

func traced(ctx context.Context, id int64, source string) context.Context {
	if _, ok := ctx.Value(traceKey{}).(trace); ok {
		return ctx
	}
	return context.WithValue(ctx, traceKey{}, trace{strconv.FormatInt(time.Now().UnixNano(), 36), source, id})
}

type LogMeta struct {
	RunID      string         `json:"run_id,omitempty"`
	Source     string         `json:"source,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
	ErrorCode  any            `json:"error_code,omitempty"`
}

func safeData(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return json.RawMessage(`{"invalid_json":true}`)
	}
	var clean func(any) any
	clean = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, v := range t {
				key := strings.ToLower(k)
				if strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "cookie") || strings.Contains(key, "secret") || strings.Contains(key, "private_key") || key == "authorization" || key == "ticket" {
					t[k] = "[redacted]"
				} else {
					t[k] = clean(v)
				}
			}
		case []any:
			for i, v := range t {
				t[i] = clean(v)
			}
		}
		return x
	}
	b, _ := json.Marshal(clean(v))
	if len(b) > 32768 {
		b, _ = json.Marshal(map[string]any{"truncated": true, "original_bytes": len(b), "preview": string(b[:32768])})
	}
	return b
}
func (e *Engine) event(ctx context.Context, id int64, action, status, message string, data any) {
	t, _ := ctx.Value(traceKey{}).(trace)
	entry := Log{Action: action, Status: status, Message: message, Meta: LogMeta{RunID: t.ID, Source: t.Source}}
	if data != nil {
		b, _ := json.Marshal(data)
		entry.Data = safeData(b)
	}
	fresh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Store.AppendLog(fresh, id, entry); err != nil {
		slog.Error("保存诊断日志失败", "self_id", id, "action", action, "error", err)
	}
	if status == "error" || status == "unknown" {
		slog.Error(message, "self_id", id, "action", action, "run_id", t.ID)
	} else {
		slog.Info(message, "self_id", id, "action", action, "status", status, "run_id", t.ID)
	}
}
func (e *Engine) decision(ctx context.Context, a *Account, action, message string, data any) {
	a.State.Message = message
	e.event(ctx, a.SelfID, action, "skip", message, data)
}
func (e *Engine) call(ctx context.Context, id int64, action string, params map[string]any) (json.RawMessage, error) {
	t, _ := ctx.Value(traceKey{}).(trace)
	if id > 0 && t.ID != "" {
		e.event(ctx, id, action, "request", "调用萌卡接口", map[string]any{"params": params})
	}
	at := time.Now()
	data, err := e.API.Call(ctx, action, params)
	if id > 0 && t.ID != "" {
		entry := Log{Action: action, Status: "response", Message: "接口查询成功", Data: safeData(data), Meta: LogMeta{RunID: t.ID, Source: t.Source, Params: params, DurationMS: time.Since(at).Milliseconds()}}
		if spec, ok := petapi.Find(action); ok && spec.Write {
			entry.Message = "写入接口已返回，提交与业务效果以回执为准"
		}
		if err != nil {
			entry.Status = "error"
			entry.Message = err.Error()
			var rpc *petapi.RPCError
			if errors.As(err, &rpc) {
				entry.Meta.ErrorCode = rpc.Code
			}
		}
		fresh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if saveErr := e.Store.AppendLog(fresh, id, entry); saveErr != nil {
			slog.Error("保存接口日志失败", "error", saveErr, "action", action)
		}
		cancel()
	}
	return data, err
}
func (e *Engine) Check(ctx context.Context, id int64) error {
	return e.Tick(traced(ctx, id, "manual_check"), id)
}

type SchedulerStatus struct {
	Running         bool   `json:"running"`
	LastCycle       string `json:"last_cycle"`
	EnabledAccounts int    `json:"enabled_accounts"`
	LastError       string `json:"last_error"`
}

func (e *Engine) Status() SchedulerStatus { e.stats.Lock(); defer e.stats.Unlock(); return e.scheduler }
