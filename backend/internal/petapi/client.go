// Package petapi implements the Mengka WebSocket contract. Requests are never
// retried here: a disconnected write may already have reached QQ.
package petapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type API interface {
	Call(context.Context, string, map[string]any) (json.RawMessage, error)
}
type RPCError struct {
	Code    any
	Message string
}

func (e *RPCError) Error() string { return fmt.Sprintf("萌卡 API 错误 %v：%s", e.Code, e.Message) }

type UnknownOutcome struct{ Cause error }

func (e *UnknownOutcome) Error() string {
	return "请求结果未知，请先核对状态：" + e.Cause.Error()
}
func (e *UnknownOutcome) Unwrap() error { return e.Cause }

var ErrDisconnected = errors.New("萌卡连接未就绪")

type reply struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	OK        *bool           `json:"ok"`
	Status    string          `json:"status"`
	Retcode   int             `json:"retcode"`
	ErrorCode any             `json:"error_code"`
	Message   string          `json:"message"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
}
type connection struct {
	ws   *websocket.Conn
	done chan struct{}
}
type Client struct {
	host           string
	port           int
	token, version string
	mu             sync.Mutex
	write          sync.Mutex
	conn           *connection
	pending        map[string]chan reply
	seq            atomic.Uint64
}

func New(host string, port int, token, version string) *Client {
	return &Client{host: host, port: port, token: token, version: version, pending: make(map[string]chan reply)}
}
func (c *Client) Connected() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.conn != nil }
func (c *Client) Run(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("萌卡连接中断，等待重连", "error", err, "retry_seconds", delay.Seconds())
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}
func (c *Client) connect(ctx context.Context) error {
	ws, _, err := (&websocket.Dialer{HandshakeTimeout: 8 * time.Second}).DialContext(ctx, "ws://"+net.JoinHostPort(c.host, strconv.Itoa(c.port))+"/", nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	ws.SetReadLimit(8 << 20)
	_ = ws.SetWriteDeadline(time.Now().Add(8 * time.Second))
	if err = ws.WriteJSON(map[string]any{"type": "auth", "token": c.token, "name": "QQ 宠物自动化", "version": c.version, "author": "e4je", "permissions": map[string]bool{}}); err != nil {
		return err
	}
	_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
	var auth reply
	if err = ws.ReadJSON(&auth); err != nil {
		return err
	}
	if auth.Type != "auth_ok" {
		return errors.New("框架认证失败")
	}
	slog.Info("萌卡 WebSocket 认证成功")
	_ = ws.SetReadDeadline(time.Now().Add(65 * time.Second))
	current := &connection{ws: ws, done: make(chan struct{})}
	c.mu.Lock()
	c.conn = current
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.conn == current {
			c.conn = nil
		}
		close(current.done)
		c.mu.Unlock()
	}()
	readErr := make(chan error, 1)
	go func() {
		for {
			var response reply
			if err := ws.ReadJSON(&response); err != nil {
				readErr <- err
				return
			}
			_ = ws.SetReadDeadline(time.Now().Add(65 * time.Second))
			c.mu.Lock()
			ch := c.pending[response.ID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- response:
				default:
				}
			}
		}
	}()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case <-heartbeat.C:
			c.write.Lock()
			_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err = ws.WriteJSON(map[string]string{"type": "ping"})
			c.write.Unlock()
			if err != nil {
				return err
			}
		}
	}
}
func (c *Client) Call(ctx context.Context, action string, params map[string]any) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	c.mu.Lock()
	current := c.conn
	c.mu.Unlock()
	if current == nil {
		return nil, ErrDisconnected
	}
	id := strconv.FormatUint(c.seq.Add(1), 10)
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	c.write.Lock()
	_ = current.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := current.ws.WriteJSON(map[string]any{"type": "action", "id": id, "action": action, "params": params})
	c.write.Unlock()
	if err != nil {
		return nil, &UnknownOutcome{err}
	}
	select {
	case <-ctx.Done():
		return nil, &UnknownOutcome{ctx.Err()}
	case <-current.done:
		return nil, &UnknownOutcome{ErrDisconnected}
	case response := <-ch:
		if response.Type != "action_result" || response.OK == nil {
			return nil, &UnknownOutcome{errors.New("框架缺少 action_result/ok 回执")}
		}
		if response.OK != nil && !*response.OK || response.Status != "" && response.Status != "ok" || response.Retcode != 0 || response.ErrorCode != nil && fmt.Sprint(response.ErrorCode) != "0" && fmt.Sprint(response.ErrorCode) != "" {
			message := response.Message
			if message == "" {
				message = response.Error
			}
			code := response.ErrorCode
			if code == nil {
				code = response.Retcode
			}
			return nil, &RPCError{code, message}
		}
		if len(response.Data) == 0 || !json.Valid(response.Data) {
			return nil, &UnknownOutcome{errors.New("框架返回无效 data")}
		}
		return response.Data, nil
	}
}
