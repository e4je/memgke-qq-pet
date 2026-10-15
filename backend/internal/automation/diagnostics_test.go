package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCareReadFailureDoesNotBlockGrowthPlan(t *testing.T) {
	f := &fakeAPI{handler: func(action string, p map[string]any) (json.RawMessage, error) {
		switch action {
		case "get_pet_vitals":
			return json.RawMessage(`{"hunger":0,"cleanliness":80}`), nil
		case "get_pet_food_catalog":
			return nil, errors.New("food directory unavailable")
		case "get_pet_activity_options":
			return json.RawMessage(`{"options":[{"name":"job","sub_event_type":6401,"can_do":true}]}`), nil
		case "start_pet_activity":
			return json.RawMessage(`{"submitted":true,"story_id":"6400_job"}`), nil
		}
		if b, ok := basic(action); ok {
			return b, nil
		}
		return nil, errors.New(action)
	}}
	e, s := setup(t, f)
	a, _ := s.Account(context.Background(), 123)
	a.Config.Care.Feed = true
	a.Config.Plans = []Plan{{ID: "job", Activity: "work", Count: 1}}
	if err := e.Configure(context.Background(), 123, a.Config, a.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	if f.count("start_pet_activity") != 1 {
		t.Fatal("growth plan starved by care read failure")
	}
	logs, err := s.Logs(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	var traceID string
	warning, request, response := false, false, false
	for _, l := range logs {
		if l.Status == "warning" && strings.Contains(l.Message, "继续检查成长") {
			warning = true
			traceID = l.Meta.RunID
		}
		if l.Action == "start_pet_activity" && l.Status == "request" {
			request = true
		}
		if l.Action == "start_pet_activity" && l.Status == "response" && l.Meta.Params["option_name"] == "job" {
			response = true
		}
	}
	if !warning || !request || !response || traceID == "" {
		t.Fatal("missing actionable diagnostics")
	}
	filtered, _ := s.QueryLogs(context.Background(), 123, LogQuery{RunID: traceID, Limit: 100})
	if len(filtered) < 8 {
		t.Fatal("trace lost API calls")
	}
}
func TestUnknownFoodStockFeedsOnceWithoutBuying(t *testing.T) {
	f := &fakeAPI{handler: func(action string, p map[string]any) (json.RawMessage, error) {
		switch action {
		case "get_pet_vitals":
			return json.RawMessage(`{"hunger":0,"cleanliness":80}`), nil
		case "get_pet_food_catalog":
			return json.RawMessage(`{"inventory_known":false,"biscuit_count":null,"foods":[]}`), nil
		case "feed_pet":
			return json.RawMessage(`{"submitted":true,"effect_verified":false}`), nil
		}
		if b, ok := basic(action); ok {
			return b, nil
		}
		return nil, errors.New(action)
	}}
	e, s := setup(t, f)
	a, _ := s.Account(context.Background(), 123)
	a.Config.Care.Feed = true
	a.Config.Care.BuyFood = true
	_ = e.Configure(context.Background(), 123, a.Config, a.Revision)
	for i := 0; i < 5; i++ {
		_ = e.Tick(context.Background(), 123)
	}
	if f.count("feed_pet") != 1 || f.count("buy_pet_food") != 0 {
		t.Fatal("unknown stock triggered purchase or repeated feed")
	}
	a, _ = s.Account(context.Background(), 123)
	if a.State.Pending == nil {
		t.Fatal("unverified feed should pause after readback")
	}
}
func TestConfigureWakesRunningScheduler(t *testing.T) {
	f := &fakeAPI{handler: func(action string, p map[string]any) (json.RawMessage, error) {
		if b, ok := basic(action); ok {
			return b, nil
		}
		if action == "get_pet_activity_options" {
			return json.RawMessage(`{"options":[{"name":"job","sub_event_type":6401,"can_do":true}]}`), nil
		}
		if action == "start_pet_activity" {
			return json.RawMessage(`{"submitted":true,"story_id":"6400_job"}`), nil
		}
		return nil, errors.New(action)
	}}
	e, s := setup(t, f)
	e.Start()
	defer e.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for f.count("get_pet_activity_status") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	a, _ := s.Account(context.Background(), 123)
	a.Config.Plans = []Plan{{ID: "job", Activity: "work", Count: 1}}
	a.State.NextRun = now().Add(time.Hour).Format(time.RFC3339)
	_ = s.Save(context.Background(), 123, a.State, nil)
	if err := e.Configure(context.Background(), 123, a.Config, a.Revision); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for f.count("start_pet_activity") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.count("start_pet_activity") != 1 || !e.Status().Running {
		t.Fatal("configuration did not wake scheduler")
	}
}
func TestLogPaginationIsolationAndRedaction(t *testing.T) {
	_, s := setup(t, &fakeAPI{})
	ctx := context.Background()
	for i := 0; i < 220; i++ {
		if err := s.AppendLog(ctx, 123, Log{Action: "test", Status: "info", Message: "entry", Data: json.RawMessage(`{"token":"secret","nested":{"password":"secret"},"hunger":1}`), Meta: LogMeta{RunID: "trace-a"}}); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.AppendLog(ctx, 999, Log{Action: "other", Status: "error", Message: "not this account"})
	first, err := s.QueryLogs(ctx, 123, LogQuery{Limit: 200})
	if err != nil || len(first) != 200 {
		t.Fatal(len(first), err)
	}
	second, _ := s.QueryLogs(ctx, 123, LogQuery{Before: first[len(first)-1].ID, Limit: 200})
	if len(second) != 20 {
		t.Fatal("bad pagination", len(second))
	}
	if strings.Contains(string(first[0].Data), "secret") || first[0].Meta.RunID != "trace-a" {
		t.Fatal("redaction or metadata failed")
	}
	if rows, _ := s.QueryLogs(ctx, 123, LogQuery{Action: "other"}); len(rows) != 0 {
		t.Fatal("cross-account logs")
	}
}

func TestSchoolUsesCurrentStageBeforeCourseDirectory(t *testing.T) {
	f := &fakeAPI{handler: func(action string, p map[string]any) (json.RawMessage, error) {
		if b, ok := basic(action); ok {
			return b, nil
		}
		switch action {
		case "get_pet_activity_overview":
			return json.RawMessage(`{"current_stage":2}`), nil
		case "get_pet_activity_options":
			if p["stage"] != 2 {
				t.Fatalf("missing real school stage: %v", p)
			}
			return json.RawMessage(`{"options":[{"name":"数学","sub_event_type":6301,"can_do":true}]}`), nil
		case "start_pet_activity":
			if p["option_name"] != "数学" || p["sub_event_type"] != 6301 {
				t.Fatal(p)
			}
			return json.RawMessage(`{"submitted":true,"story_id":"6300_school"}`), nil
		}
		return nil, errors.New(action)
	}}
	e, s := setup(t, f)
	a, _ := s.Account(context.Background(), 123)
	a.Config.Plans = []Plan{{ID: "school", Activity: "school", Count: 1}}
	_ = e.Configure(context.Background(), 123, a.Config, a.Revision)
	if err := e.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	if f.count("start_pet_activity") != 1 {
		t.Fatal("school plan did not start")
	}
}

func TestExistingLogTableIsUpgradedWithoutLosingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE pet_v2_logs(id INTEGER PRIMARY KEY AUTOINCREMENT,self_id INTEGER NOT NULL,action TEXT NOT NULL,status TEXT NOT NULL,message TEXT NOT NULL,data TEXT,at TEXT NOT NULL);INSERT INTO pet_v2_logs(self_id,action,status,message,data,at) VALUES(123,'feed_pet','submitted','old receipt','{"submitted":true}','2026-10-15T00:00:00+08:00');`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	logs, err := s.Logs(context.Background(), 123)
	if err != nil || len(logs) != 1 || logs[0].Message != "old receipt" {
		t.Fatal("lost historical log", err)
	}
	if err = s.AppendLog(context.Background(), 123, Log{Action: "tick", Status: "info", Message: "new receipt", Meta: LogMeta{RunID: "new-trace"}}); err != nil {
		t.Fatal(err)
	}
	logs, err = s.Logs(context.Background(), 123)
	if err != nil || len(logs) != 2 || logs[0].Meta.RunID != "new-trace" {
		t.Fatal("metadata migration failed", err)
	}
}
