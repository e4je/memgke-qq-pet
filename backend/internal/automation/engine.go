package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"qq-pet/backend/internal/petapi"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	API       petapi.API
	Store     *Store
	locks     sync.Map
	lifecycle sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
}

func New(api petapi.API, store *Store) *Engine { return &Engine{API: api, Store: store} }
func (e *Engine) lock(id int64) func() {
	v, _ := e.locks.LoadOrStore(id, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
func (e *Engine) Start() {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	if e.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	go func() {
		defer close(e.done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			e.cycle(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (e *Engine) Stop() {
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	if e.cancel != nil {
		e.cancel()
		<-e.done
		e.cancel = nil
	}
}
func (e *Engine) cycle(ctx context.Context) {
	ids, err := e.Store.Enabled(ctx)
	if err != nil {
		return
	}
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			defer func() { <-slots }()
			task, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			_ = e.Tick(task, id)
		}(id)
	}
	wg.Wait()
}

type Bot struct {
	SelfID     int64  `json:"self_id"`
	Nickname   string `json:"nickname"`
	Platform   string `json:"platform"`
	ClientType string `json:"client_type"`
	Status     int    `json:"status"`
}

func (e *Engine) Bots(ctx context.Context) ([]Bot, error) {
	b, err := e.API.Call(ctx, "get_bot_list", map[string]any{})
	if err != nil {
		return nil, err
	}
	return decode[[]Bot](b)
}
func (e *Engine) online(ctx context.Context, id int64) error {
	bots, err := e.Bots(ctx)
	if err != nil {
		return err
	}
	for _, b := range bots {
		if b.SelfID == id && (b.ClientType == "android" || b.Platform == "android") {
			if b.Status != 1 {
				return errors.New("Android 账号离线，等待框架恢复登录")
			}
			return nil
		}
	}
	return errors.New("框架中没有此 Android 账号；宠物接口不支持 Linux 协议")
}
func (e *Engine) read(ctx context.Context, id int64, action string, params map[string]any) (json.RawMessage, error) {
	return petapi.Invoke(ctx, e.API, id, action, params)
}
func (e *Engine) Configure(ctx context.Context, id int64, c Config, revision int) error {
	unlock := e.lock(id)
	defer unlock()
	return e.Store.Configure(ctx, id, c, revision)
}

// persist uses a fresh bounded context even after a request timeout so an
// ambiguous write remains visible after restart instead of silently retrying.
func (e *Engine) persist(id int64, s State, l *Log) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return e.Store.Save(ctx, id, s, l)
}
func (e *Engine) write(ctx context.Context, id int64, s *State, action string, p map[string]any, apply func(json.RawMessage) error) (json.RawMessage, error) {
	params, err := petapi.Prepare(id, action, p)
	if err != nil {
		return nil, err
	}
	if s.Pending != nil {
		return nil, errors.New("存在待核对请求，已暂停写入")
	}
	s.Pending = &Pending{Action: action, Params: params, At: now().Format(time.RFC3339), Reason: "请求已登记，尚未确认结果"}
	if err = e.persist(id, *s, nil); err != nil {
		return nil, err
	}
	data, err := e.API.Call(ctx, action, params)
	if err != nil {
		var unknown *petapi.UnknownOutcome
		var rpc *petapi.RPCError
		if errors.As(err, &unknown) || errors.As(err, &rpc) {
			// An action error can wrap an upstream timeout after QQ accepted the
			// request. With no idempotency key, inspect state before any retry.
			s.Pending.Reason = err.Error()
		} else {
			s.Pending = nil
		}
		s.LastError = err.Error()
		s.Message = "操作失败；请查看日志"
		saveErr := e.persist(id, *s, &Log{Action: action, Status: "error", Message: err.Error()})
		if saveErr != nil {
			return nil, saveErr
		}
		return nil, err
	}
	var receipt struct {
		Submitted      *bool `json:"submitted"`
		EffectVerified *bool `json:"effect_verified"`
	}
	if json.Unmarshal(data, &receipt) != nil || receipt.Submitted == nil || !*receipt.Submitted {
		err = errors.New("写入未返回 submitted=true，已暂停等待人工核对")
	} else if apply != nil {
		err = apply(data)
	}
	if err != nil {
		s.Pending.Reason = err.Error()
		s.LastError = err.Error()
		_ = e.persist(id, *s, &Log{Action: action, Status: "unknown", Message: err.Error(), Data: data})
		return nil, err
	}
	intent := s.Pending
	s.Pending = nil
	s.LastWrite = now().Format(time.RFC3339)
	s.LastError = ""
	s.Failures = 0
	status := "submitted"
	message := "请求已提交，业务效果尚未核验"
	if receipt.EffectVerified != nil && *receipt.EffectVerified {
		status = "verified"
		message = "接口已核验业务效果"
	}
	s.Message = message
	if err = e.persist(id, *s, &Log{Action: action, Status: status, Message: message, Data: data}); err != nil {
		s.Pending = intent
		s.Pending.Reason = "操作已提交，但结果持久化失败；请核对后恢复"
		return nil, err
	}
	return data, nil
}
func (e *Engine) Action(ctx context.Context, id int64, action string, p map[string]any) (json.RawMessage, error) {
	spec, ok := petapi.Find(action)
	if !ok {
		return nil, errors.New("接口不在官方宠物清单中")
	}
	unlock := e.lock(id)
	defer unlock()
	if err := e.online(ctx, id); err != nil {
		return nil, err
	}
	if !spec.Write {
		return e.read(ctx, id, action, p)
	}
	a, err := e.Store.Account(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.State.Task != nil && (action == "start_pet_activity" || action == "start_pet_pk") {
		return nil, errors.New("当前存在已跟踪任务，请先完成或核对该任务")
	}
	return e.write(ctx, id, &a.State, action, p, func(b json.RawMessage) error {
		if action == "start_pet_activity" || action == "start_pet_pk" {
			var r struct {
				StoryID string `json:"story_id"`
			}
			if json.Unmarshal(b, &r) != nil || r.StoryID == "" {
				return errors.New("发起任务后缺少 story_id")
			}
			kind := "manual"
			if action == "start_pet_pk" {
				if !strings.HasPrefix(r.StoryID, "6900_") {
					return errors.New("PK 启动未返回有效 story_id")
				}
				kind = "pk"
			}
			a.State.Task = &Task{Kind: kind, PetID: fmt.Sprint(p["pet_id"]), StoryID: r.StoryID}
		}
		if (action == "settle_pet_activity" || action == "settle_pet_pk") && a.State.Task != nil && p["story_id"] == a.State.Task.StoryID {
			a.State.Task.Settling = true
		}
		return nil
	})
}
func (e *Engine) Resolve(ctx context.Context, id int64, storyID string, clearTask bool) error {
	unlock := e.lock(id)
	defer unlock()
	a, err := e.Store.Account(ctx, id)
	if err != nil {
		return err
	}
	if a.State.Pending == nil && !clearTask {
		return errors.New("没有待核对请求")
	}
	// A recovered task ID must be checked through the appropriate official API.
	if storyID != "" {
		pending := a.State.Pending
		if pending == nil {
			return errors.New("没有可恢复的发起请求")
		}
		if pending.Action != "start_pet_pk" && pending.Action != "start_pet_activity" {
			return errors.New("此待确认操作不是任务启动")
		}
		pet := fmt.Sprint(pending.Params["pet_id"])
		kind := "manual"
		action := "get_pet_activity_status"
		p := map[string]any{"pet_id": pet}
		if pending.Action == "start_pet_pk" {
			kind = "pk"
			action = "get_pet_pk_status"
			p["story_id"] = storyID
		}
		data, err := e.read(ctx, id, action, p)
		if err != nil {
			return err
		}
		var result struct {
			StoryID string `json:"story_id"`
		}
		if json.Unmarshal(data, &result) != nil || result.StoryID != storyID {
			return errors.New("当前官方任务 ID 与输入不一致")
		}
		a.State.Task = &Task{Kind: kind, PetID: pet, StoryID: storyID}
	}
	if clearTask {
		a.State.Task = nil
	}
	a.State.Pending = nil
	a.State.Verify = nil
	a.State.LastError = ""
	a.State.NextRun = ""
	a.Config.Enabled = false
	// Confirmation never silently resumes automatic side effects.
	if a.Revision > 0 {
		if err = e.Store.Configure(ctx, id, a.Config, a.Revision); err != nil {
			return err
		}
	}
	return e.persist(id, a.State, &Log{Action: "resolve", Status: "confirmed", Message: "管理员已核对；自动化保持关闭，请手动启用"})
}
func (e *Engine) Tick(ctx context.Context, id int64) error {
	unlock := e.lock(id)
	defer unlock()
	a, err := e.Store.Account(ctx, id)
	if err != nil {
		return err
	}
	if !a.Config.Enabled {
		return nil
	}
	s := &a.State
	if s.Pending != nil {
		s.Message = "有待核对的写入请求，自动化已暂停"
		return e.persist(id, *s, nil)
	}
	if next, err := time.Parse(time.RFC3339, s.NextRun); err == nil && now().Before(next) {
		return nil
	}
	err = e.step(ctx, &a)
	if err != nil {
		s.Failures++
		s.LastError = err.Error()
		s.Message = "等待重试：" + err.Error()
		delay := time.Duration(min(30*(1<<min(s.Failures-1, 4)), 300)) * time.Second
		s.NextRun = now().Add(delay).Format(time.RFC3339)
	} else {
		s.Failures = 0
		s.NextRun = ""
		s.LastError = ""
	}
	saveErr := e.persist(id, *s, nil)
	if saveErr != nil {
		return saveErr
	}
	return err
}

type activityStatus struct {
	StoryID   string `json:"story_id"`
	Finished  *bool  `json:"finished"`
	StateCode int    `json:"state_code"`
	Remaining int    `json:"remaining_seconds"`
}
type vitals struct {
	Hunger      *float64 `json:"hunger"`
	Cleanliness *float64 `json:"cleanliness"`
	Gold        *float64 `json:"gold"`
}

func (e *Engine) step(ctx context.Context, a *Account) error {
	id, c, s := a.SelfID, a.Config, &a.State
	if err := e.online(ctx, id); err != nil {
		return err
	}
	today := now().Format("2006-01-02")
	if s.Date != today {
		s.Date = today
		s.Counts = map[string]int{}
		s.LastSocial = ""
	}
	profile, err := e.read(ctx, id, "get_pet_profile", nil)
	if err != nil {
		return err
	}
	var own struct {
		PetID string `json:"pet_id"`
	}
	if json.Unmarshal(profile, &own) != nil || own.PetID == "" {
		return errors.New("本人档案没有真实 pet_id")
	}
	s.Profile = profile
	p := map[string]any{"pet_id": own.PetID}
	raw, err := e.read(ctx, id, "get_pet_vitals", p)
	if err != nil {
		return err
	}
	v, err := decode[vitals](raw)
	if err != nil {
		return err
	}
	s.Vitals = raw
	if s.Verify != nil {
		return e.verify(ctx, a)
	}
	if s.Task != nil {
		previousWrite := s.LastWrite
		if err := e.advance(ctx, a); err != nil {
			return err
		}
		if s.Task == nil || s.Pending != nil || s.LastWrite != previousWrite {
			return nil
		}
		// Long-running work must not starve daily care or friend interactions.
		if !inWindow(now(), c.Start, c.End) {
			return nil
		}
		if last, err := time.Parse(time.RFC3339, s.LastWrite); err == nil && now().Sub(last) < 60*time.Second {
			return nil
		}
		if did, err := e.care(ctx, a, own.PetID, v); did || err != nil {
			return err
		}
		_, err := e.social(ctx, a)
		return err
	}
	if !inWindow(now(), c.Start, c.End) {
		s.Message = "等待运行时间段"
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s.LastWrite); err == nil && now().Sub(t) < 60*time.Second {
		s.Message = "等待操作冷却"
		return nil
	}
	if did, err := e.care(ctx, a, own.PetID, v); did || err != nil {
		return err
	}
	raw, err = e.read(ctx, id, "get_pet_activity_status", p)
	if err != nil {
		return err
	}
	activity, err := decode[activityStatus](raw)
	if err != nil {
		return err
	}
	if activity.Finished == nil {
		return errors.New("活动状态缺少 finished，无法确认空闲")
	}
	if activity.StoryID != "" {
		s.Message = "检测到未由本插件发起的活动，请在接口面板核对或结算"
		return nil
	}
	for _, plan := range c.Plans {
		if s.Counts["plan:"+plan.ID] >= plan.Count {
			continue
		}
		if c.PauseFatigued && (plan.Activity == "school" || plan.Activity == "work") {
			raw, err = e.read(ctx, id, "get_pet_fatigue_status", p)
			if err != nil {
				return err
			}
			var fatigue struct {
				Known    bool `json:"known"`
				Fatigued bool `json:"fatigued"`
			}
			if json.Unmarshal(raw, &fatigue) != nil {
				return errors.New("疲劳状态格式错误")
			}
			if !fatigue.Known || fatigue.Fatigued {
				s.Message = "疲劳状态未知或已疲劳，暂缓学习/工作"
				continue
			}
		}
		options := map[string]any{"pet_id": own.PetID, "activity": plan.Activity}
		if plan.Stage != nil {
			options["stage"] = *plan.Stage
		}
		if plan.CareerType != nil {
			options["career_type"] = *plan.CareerType
		}
		if plan.FriendPetID != "" {
			options["friend_pet_id"] = plan.FriendPetID
		}
		raw, err = e.read(ctx, id, "get_pet_activity_options", options)
		if err != nil {
			return err
		}
		var catalog struct {
			Options []struct {
				Name     string `json:"name"`
				SubEvent int    `json:"sub_event_type"`
				CanDo    bool   `json:"can_do"`
			} `json:"options"`
		}
		if json.Unmarshal(raw, &catalog) != nil {
			return errors.New("活动目录格式错误")
		}
		for _, option := range catalog.Options {
			if !option.CanDo || plan.Option != "" && plan.Option != option.Name {
				continue
			}
			params := map[string]any{"pet_id": own.PetID, "activity": plan.Activity, "option_name": option.Name, "sub_event_type": option.SubEvent}
			if plan.FriendUIN != "" {
				params["friend_uin"] = plan.FriendUIN
				params["friend_pet_id"] = plan.FriendPetID
			}
			_, err = e.write(ctx, id, s, "start_pet_activity", params, func(b json.RawMessage) error {
				var result struct {
					StoryID string `json:"story_id"`
				}
				if json.Unmarshal(b, &result) != nil || result.StoryID == "" {
					return errors.New("活动启动未返回 story_id")
				}
				s.Task = &Task{Kind: plan.Activity, PlanID: plan.ID, PetID: own.PetID, StoryID: result.StoryID}
				return nil
			})
			return err
		}
		s.Message = "活动目录无可执行选项，保留计划等待下一轮"
	}
	if did, err := e.social(ctx, a); did || err != nil {
		return err
	}
	if c.PK.Enabled && s.Counts["pk"] < c.PK.DailyLimit {
		return e.startPK(ctx, a, own.PetID)
	}
	if s.Message == "" || strings.HasPrefix(s.Message, "请求") {
		s.Message = "今日任务已完成或未配置可执行任务"
	}
	return nil
}
func (e *Engine) advance(ctx context.Context, a *Account) error {
	s, t := &a.State, a.State.Task
	p := map[string]any{"pet_id": t.PetID}
	action := "get_pet_activity_status"
	if t.Kind == "pk" {
		action = "get_pet_pk_status"
		p["story_id"] = t.StoryID
	}
	if t.Kind == "pk" && t.Settling {
		// settle_pet_pk only acknowledges submission. Keep the receipt and refresh
		// vitals without replaying settlement; never invent a verified reward.
		data, err := e.read(ctx, a.SelfID, "get_pet_vitals", map[string]any{"pet_id": t.PetID})
		if err != nil {
			return err
		}
		s.Vitals = data
		s.Task = nil
		s.Message = "PK 结算已提交，已刷新数值；奖励以官方返回为准"
		return nil
	}
	raw, err := e.read(ctx, a.SelfID, action, p)
	if err != nil {
		return err
	}
	status, err := decode[activityStatus](raw)
	if err != nil {
		return err
	}
	if status.Finished == nil {
		return errors.New("状态缺少 finished，不能推断任务完成")
	}
	if t.Kind != "pk" && status.StoryID != t.StoryID {
		if t.Settling {
			if t.PlanID != "" {
				s.Counts["plan:"+t.PlanID]++
			}
			s.Task = nil
			s.Message = "活动结算后状态已更新"
			return nil
		}
		s.Pending = &Pending{Action: "reconcile_activity", At: now().Format(time.RFC3339), Reason: "当前任务与已跟踪 story_id 不一致，不能认定已完成"}
		return errors.New(s.Pending.Reason)
	}
	if t.Kind == "pk" {
		if status.StoryID != t.StoryID {
			return errors.New("PK 状态返回的 story_id 不一致")
		}
		var pk struct {
			Known bool `json:"status_known"`
		}
		if json.Unmarshal(raw, &pk) != nil || !pk.Known {
			return errors.New("PK 状态未知，等待下一轮查询")
		}
	}
	if status.Finished == nil {
		return errors.New("状态缺少 finished，不能推断任务完成")
	}
	if !*status.Finished {
		if a.Config.Encourage && t.Kind != "pk" && t.Kind != "adventure" && !t.Encouraged {
			_, err = e.write(ctx, a.SelfID, s, "encourage_pet_activity", map[string]any{"pet_id": t.PetID, "story_id": t.StoryID}, func(json.RawMessage) error { t.Encouraged = true; return nil })
			return err
		}
		s.Message = "任务进行中，等待官方完成状态"
		return nil
	}
	if t.Settling {
		s.Message = "结算已提交，等待官方状态更新；不会重复结算"
		return nil
	}
	action = "settle_pet_activity"
	if t.Kind == "pk" {
		action = "settle_pet_pk"
	}
	_, err = e.write(ctx, a.SelfID, s, action, map[string]any{"pet_id": t.PetID, "story_id": t.StoryID}, func(json.RawMessage) error { t.Settling = true; return nil })
	return err
}

type friend struct {
	PetID  string `json:"pet_id"`
	UserID string `json:"user_id"`
	Name   string `json:"pet_name"`
}

func (e *Engine) directory(ctx context.Context, id int64, source string) ([]friend, error) {
	action := "get_pet_pk_friends"
	if source == "strangers" {
		action = "get_pet_pk_strangers"
	}
	cursor := ""
	seen := map[string]bool{}
	out := []friend{}
	for page := 0; page < 5; page++ {
		p := map[string]any{}
		if cursor != "" {
			p["cursor"] = cursor
		}
		raw, err := e.read(ctx, id, action, p)
		if err != nil {
			return nil, err
		}
		var result struct {
			Friends []friend `json:"friends"`
			Next    string   `json:"next_cursor"`
			More    bool     `json:"has_more"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return nil, errors.New("宠物目录格式错误")
		}
		for _, f := range result.Friends {
			if _, err := strconv.ParseInt(f.UserID, 10, 64); err == nil && f.PetID != "" && f.UserID != strconv.FormatInt(id, 10) && !seen[f.UserID] {
				seen[f.UserID] = true
				out = append(out, f)
			}
		}
		if !result.More || result.Next == "" || result.Next == cursor {
			break
		}
		cursor = result.Next
	}
	return out, nil
}
func (e *Engine) social(ctx context.Context, a *Account) (bool, error) {
	c, s := a.Config.Social, &a.State
	if len(c.Actions) == 0 || s.Counts["social"] >= c.DailyLimit {
		return false, nil
	}
	if last, err := time.Parse(time.RFC3339, s.LastSocial); err == nil && now().Sub(last) < time.Duration(c.IntervalSeconds)*time.Second {
		return false, nil
	}
	friends, err := e.directory(ctx, a.SelfID, "friends")
	if err != nil {
		return true, err
	}
	for _, f := range friends {
		for _, action := range c.Actions {
			key := "social:" + action + ":" + f.UserID
			if s.Counts[key] > 0 {
				continue
			}
			p := map[string]any{"friend_uin": f.UserID}
			if action != "poke_friend_pet" {
				p["pet_id"] = f.PetID
			}
			if action == "feed_friend_pet" && c.FoodID != "" {
				p["food_id"] = c.FoodID
			}
			if action == "bathe_friend_pet" {
				p["item_id"] = c.ItemID
				p["count"] = 1
			}
			_, err = e.write(ctx, a.SelfID, s, action, p, func(json.RawMessage) error {
				s.Counts[key]++
				s.Counts["social"]++
				s.LastSocial = now().Format(time.RFC3339)
				return nil
			})
			return true, err
		}
	}
	return false, nil
}
func (e *Engine) startPK(ctx context.Context, a *Account, pet string) error {
	s, c := &a.State, a.Config.PK
	friends, err := e.directory(ctx, a.SelfID, c.Source)
	if err != nil {
		return err
	}
	power := func(pet string) (float64, error) {
		raw, err := e.read(ctx, a.SelfID, "get_pet_pk_power", map[string]any{"pet_id": pet})
		if err != nil {
			return 0, err
		}
		var p struct {
			Power *float64 `json:"power"`
		}
		if json.Unmarshal(raw, &p) != nil || p.Power == nil {
			return 0, errors.New("缺少实时战力")
		}
		return *p.Power, nil
	}
	own, err := power(pet)
	if err != nil {
		return err
	}
	for _, f := range friends {
		key := "pk:" + f.UserID
		if s.Counts[key] >= c.PerOpponent {
			continue
		}
		if c.StrongerOnly {
			opponent, err := power(f.PetID)
			if err != nil {
				return err
			}
			if own <= opponent {
				continue
			}
		}
		_, err = e.write(ctx, a.SelfID, s, "start_pet_pk", map[string]any{"pet_id": pet, "friend_uin": f.UserID, "friend_pet_id": f.PetID}, func(raw json.RawMessage) error {
			var r struct {
				StoryID string `json:"story_id"`
			}
			if json.Unmarshal(raw, &r) != nil || !strings.HasPrefix(r.StoryID, "6900_") {
				return errors.New("PK 启动缺少有效 story_id")
			}
			s.Task = &Task{Kind: "pk", PetID: pet, StoryID: r.StoryID}
			s.Counts["pk"]++
			s.Counts[key]++
			return nil
		})
		return err
	}
	s.Message = "本轮没有符合条件的官方 PK 对手"
	return nil
}
