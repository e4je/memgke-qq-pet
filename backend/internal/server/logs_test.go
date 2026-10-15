package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"qq-pet/backend/internal/automation"
	"strings"
	"testing"
)

func TestLogFiltersAndExport(t *testing.T) {
	store, err := automation.Open(filepath.Join(t.TempDir(), "pet.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, id := range []int64{123, 456} {
		if err = store.AppendLog(ctx, id, automation.Log{Action: "feed_pet", Status: "error", Message: "test failure", Meta: automation.LogMeta{RunID: "run-a", ErrorCode: "TEST_CODE"}, Data: json.RawMessage(`{"token":"do-not-export","hunger":30}`)}); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(automation.New(nil, store), "test-version", func() bool { return true })
	for _, path := range []string{"/api/accounts/123/logs?status=error&action=feed_pet&run_id=run-a", "/api/accounts/123/logs/export?run_id=run-a"} {
		req := httptest.NewRequest("GET", path, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != 200 || strings.Contains(res.Body.String(), "do-not-export") {
			t.Fatal(res.Code, res.Body.String())
		}
		var value map[string]json.RawMessage
		if err = json.Unmarshal(res.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		raw := value["data"]
		if strings.Contains(path, "export") {
			raw = value["logs"]
			if res.Header().Get("Content-Disposition") == "" {
				t.Fatal("missing download header")
			}
		}
		var logs []automation.Log
		if err = json.Unmarshal(raw, &logs); err != nil || len(logs) != 1 || logs[0].SelfID != 123 || logs[0].Meta.ErrorCode != "TEST_CODE" {
			t.Fatal("bad export/filter", res.Body.String(), err)
		}
	}
}
