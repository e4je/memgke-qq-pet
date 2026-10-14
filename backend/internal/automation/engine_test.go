package automation

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"qq-pet/backend/internal/petapi"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAPI struct {
	mu      sync.Mutex
	calls   []string
	handler func(string, map[string]any) (json.RawMessage, error)
}

func (f *fakeAPI) Call(_ context.Context, a string, p map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, a)
	if a == "get_bot_list" {
		return json.RawMessage(`[{"self_id":123,"platform":"android","client_type":"android","status":1}]`), nil
	}
	return f.handler(a, p)
}
func (f *fakeAPI) count(a string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, v := range f.calls {
		if v == a {
			n++
		}
	}
	return n
}
func setup(t *testing.T, f *fakeAPI) (*Engine, *Store) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "pet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c := DefaultConfig()
	c.Enabled = true
	c.PauseFatigued = false
	if err = s.Configure(context.Background(), 123, c, 0); err != nil {
		t.Fatal(err)
	}
	return New(f, s), s
}
func basic(a string) (json.RawMessage, bool) {
	switch a {
	case "get_pet_profile":
		return json.RawMessage(`{"pet_id":"real-id","pet_name":"test"}`), true
	case "get_pet_vitals":
		return json.RawMessage(`{"hunger":80,"cleanliness":80,"gold":100}`), true
	case "get_pet_activity_status":
		return json.RawMessage(`{"story_id":"","finished":false}`), true
	}
	return nil, false
}

func TestActivitySettlesOnceAndRecoversAcrossRestart(t *testing.T) {
	finished, settled := false, false
	f := &fakeAPI{handler: func(a string, p map[string]any) (json.RawMessage, error) {
		switch a {
		case "get_pet_activity_status":
			if settled {
				return json.RawMessage(`{"story_id":"","finished":false}`), nil
			}
			return json.RawMessage(`{"story_id":"6400_job","finished":` + map[bool]string{true: "true", false: "false"}[finished] + `}`), nil
		case "settle_pet_activity":
			settled = true
			return json.RawMessage(`{"submitted":true,"effect_verified":false}`), nil
		}
		if b, ok := basic(a); ok {
			return b, nil
		}
		return nil, errors.New("unexpected " + a)
	}}
	engine, store := setup(t, f)
	a, _ := store.Account(context.Background(), 123)
	a.State.Date = now().Format("2006-01-02")
	a.State.Task = &Task{Kind: "work", PlanID: "p1", PetID: "real-id", StoryID: "6400_job"}
	if err := store.Save(context.Background(), 123, a.State, nil); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	if f.count("settle_pet_activity") != 0 {
		t.Fatal("settled unfinished activity")
	}
	finished = true
	engine = New(f, store)
	if err := engine.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	engine = New(f, store)
	if err := engine.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	a, _ = store.Account(context.Background(), 123)
	if a.State.Task != nil || a.State.Counts["plan:p1"] != 1 || f.count("settle_pet_activity") != 1 {
		t.Fatalf("bad recovery: %+v", a.State)
	}
}
func TestUnknownWriteSurvivesRestartAndNeverRetries(t *testing.T) {
	f := &fakeAPI{handler: func(a string, p map[string]any) (json.RawMessage, error) {
		return nil, &petapi.UnknownOutcome{Cause: context.DeadlineExceeded}
	}}
	engine, store := setup(t, f)
	_, err := engine.Action(context.Background(), 123, "buy_pet_food", map[string]any{"count": 1})
	if err == nil {
		t.Fatal("expected uncertainty")
	}
	engine = New(f, store)
	if err = engine.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	if f.count("buy_pet_food") != 1 {
		t.Fatal("write retried")
	}
	a, _ := store.Account(context.Background(), 123)
	if a.State.Pending == nil {
		t.Fatal("lost pending write")
	}
	if err = engine.Resolve(context.Background(), 123, "", false); err != nil {
		t.Fatal(err)
	}
	a, _ = store.Account(context.Background(), 123)
	if a.Config.Enabled || a.State.Pending != nil {
		t.Fatal("resolve must leave automation disabled")
	}
}
func TestPKRequiresKnownFinishedAndSettlesOnce(t *testing.T) {
	status := `{"status_known":false,"finished":true,"story_id":"6900_test"}`
	f := &fakeAPI{handler: func(a string, p map[string]any) (json.RawMessage, error) {
		if a == "get_pet_pk_status" {
			return json.RawMessage(status), nil
		}
		if a == "settle_pet_pk" {
			return json.RawMessage(`{"submitted":true,"effect_verified":false}`), nil
		}
		if b, ok := basic(a); ok {
			return b, nil
		}
		return nil, errors.New(a)
	}}
	engine, store := setup(t, f)
	a, _ := store.Account(context.Background(), 123)
	a.State.Task = &Task{Kind: "pk", PetID: "real-id", StoryID: "6900_test"}
	_ = store.Save(context.Background(), 123, a.State, nil)
	if engine.Tick(context.Background(), 123) == nil {
		t.Fatal("unknown PK status accepted")
	}
	if f.count("settle_pet_pk") != 0 {
		t.Fatal("settled unknown PK")
	}
	a, _ = store.Account(context.Background(), 123)
	a.State.NextRun = ""
	_ = store.Save(context.Background(), 123, a.State, nil)
	status = `{"status_known":true,"finished":true,"story_id":"6900_test"}`
	if err := engine.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	engine = New(f, store)
	if err := engine.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	if f.count("settle_pet_pk") != 1 {
		t.Fatal("settlement repeated")
	}
}
func TestInventoryUnknownNeverTriggersPurchase(t *testing.T) {
	f := &fakeAPI{handler: func(a string, p map[string]any) (json.RawMessage, error) {
		if a == "get_pet_vitals" {
			return json.RawMessage(`{"hunger":0,"cleanliness":80}`), nil
		}
		if a == "get_pet_food_catalog" {
			return json.RawMessage(`{"biscuit_count":null,"foods":[],"inventory_known":false}`), nil
		}
		if b, ok := basic(a); ok {
			return b, nil
		}
		return nil, errors.New(a)
	}}
	engine, store := setup(t, f)
	a, _ := store.Account(context.Background(), 123)
	a.Config.Care.Feed = true
	a.Config.Care.BuyFood = true
	_ = store.Configure(context.Background(), 123, a.Config, a.Revision)
	if engine.Tick(context.Background(), 123) == nil {
		t.Fatal("unknown inventory accepted")
	}
	if f.count("buy_pet_food") != 0 {
		t.Fatal("purchased unknown inventory")
	}
}
func TestSubmittedPurchaseWithoutInventoryChangePauses(t *testing.T) {
	f := &fakeAPI{handler: func(a string, p map[string]any) (json.RawMessage, error) {
		if a == "get_pet_vitals" {
			return json.RawMessage(`{"hunger":0,"cleanliness":80}`), nil
		}
		if a == "get_pet_food_catalog" {
			return json.RawMessage(`{"biscuit_count":0}`), nil
		}
		if a == "buy_pet_food" {
			return json.RawMessage(`{"submitted":true,"effect_verified":false}`), nil
		}
		if b, ok := basic(a); ok {
			return b, nil
		}
		return nil, errors.New(a)
	}}
	engine, store := setup(t, f)
	a, _ := store.Account(context.Background(), 123)
	a.Config.Care.Feed = true
	a.Config.Care.BuyFood = true
	_ = store.Configure(context.Background(), 123, a.Config, a.Revision)
	for i := 0; i < 4; i++ {
		_ = engine.Tick(context.Background(), 123)
	}
	a, _ = store.Account(context.Background(), 123)
	if a.State.Pending == nil || f.count("buy_pet_food") != 1 {
		t.Fatalf("purchase was replayed or not paused: %+v", a.State)
	}
}
func TestConfigRevisionAndValidation(t *testing.T) {
	_, store := setup(t, &fakeAPI{})
	a, _ := store.Account(context.Background(), 123)
	if err := store.Configure(context.Background(), 123, a.Config, a.Revision); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(store.Configure(context.Background(), 123, a.Config, a.Revision), ErrRevision) {
		t.Fatal("stale revision accepted")
	}
	c := DefaultConfig()
	c.Social.Actions = []string{"send_group_msg"}
	if c.Validate() == nil {
		t.Fatal("unsupported action accepted")
	}
}
func TestTimeWindow(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 10, 14, h, 0, 0, 0, zone) }
	if !inWindow(at(23), "22:00", "06:00") || !inWindow(at(4), "22:00", "06:00") || inWindow(at(12), "22:00", "06:00") {
		t.Fatal("overnight window broken")
	}
}
func TestOfficialDirectoryErrorIsNotEmptySuccess(t *testing.T) {
	f := &fakeAPI{handler: func(a string, p map[string]any) (json.RawMessage, error) {
		return nil, &petapi.RPCError{Code: 1000120, Message: "unavailable"}
	}}
	engine, _ := setup(t, f)
	_, err := engine.directory(context.Background(), 123, "friends")
	if err == nil || !strings.Contains(err.Error(), "1000120") {
		t.Fatal("directory failure hidden")
	}
}

func TestConfirmedWriteWithFailedPersistenceStaysPending(t *testing.T) {
	var store *Store
	f := &fakeAPI{handler: func(action string, p map[string]any) (json.RawMessage, error) {
		_, err := store.DB.Exec(`CREATE TRIGGER fail_receipt BEFORE INSERT ON pet_v2_logs BEGIN SELECT RAISE(FAIL, 'disk unavailable'); END;`)
		if err != nil {
			t.Fatal(err)
		}
		return json.RawMessage(`{"submitted":true}`), nil
	}}
	engine, s := setup(t, f)
	store = s
	a, _ := store.Account(context.Background(), 123)
	_, err := engine.write(context.Background(), 123, &a.State, "buy_pet_food", map[string]any{"count": 1}, nil)
	if err == nil || a.State.Pending == nil {
		t.Fatal("receipt failure lost intent")
	}
	if err = engine.persist(123, a.State, nil); err != nil {
		t.Fatal(err)
	}
	if err = engine.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	if f.count("buy_pet_food") != 1 {
		t.Fatal("replayed after persistence error")
	}
}

func TestPlanUsesLiveOptionsAndTracksStart(t *testing.T) {
	f := &fakeAPI{handler: func(action string, p map[string]any) (json.RawMessage, error) {
		if b, ok := basic(action); ok {
			return b, nil
		}
		switch action {
		case "get_pet_activity_options":
			return json.RawMessage(`{"options":[{"name":"locked","sub_event_type":1,"can_do":false},{"name":"current job","sub_event_type":6401,"can_do":true}]}`), nil
		case "start_pet_activity":
			if p["option_name"] != "current job" || p["sub_event_type"] != 6401 || p["pet_id"] != "real-id" || p["client_type"] != "android" {
				t.Fatalf("invalid selection: %v", p)
			}
			return json.RawMessage(`{"submitted":true,"story_id":"6400_live"}`), nil
		}
		return nil, errors.New(action)
	}}
	e, s := setup(t, f)
	a, _ := s.Account(context.Background(), 123)
	a.Config.Plans = []Plan{{ID: "job", Activity: "work", Count: 1}}
	if err := s.Configure(context.Background(), 123, a.Config, a.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(context.Background(), 123); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Account(context.Background(), 123)
	if a.State.Task == nil || a.State.Task.StoryID != "6400_live" || a.State.Counts["plan:job"] != 0 {
		t.Fatal("task missing or counted before settlement")
	}
}

func TestBathUnknownIsNotZero(t *testing.T) {
	for _, payload := range []string{`null`, `[{"item_id":"1"}]`} {
		f := &fakeAPI{handler: func(string, map[string]any) (json.RawMessage, error) { return json.RawMessage(payload), nil }}
		e, _ := setup(t, f)
		if _, err := e.bathStock(context.Background(), 123, "1"); err == nil {
			t.Fatal("unknown inventory became zero")
		}
	}
}
