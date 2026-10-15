package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

type Plan struct {
	ID          string `json:"id"`
	Activity    string `json:"activity"`
	Option      string `json:"option"`
	Count       int    `json:"count"`
	Stage       *int   `json:"stage,omitempty"`
	CareerType  *int   `json:"career_type,omitempty"`
	FriendUIN   string `json:"friend_uin,omitempty"`
	FriendPetID string `json:"friend_pet_id,omitempty"`
}
type Care struct {
	Feed          bool   `json:"feed"`
	Hunger        int    `json:"hunger"`
	FoodID        string `json:"food_id"`
	Bathe         bool   `json:"bathe"`
	Cleanliness   int    `json:"cleanliness"`
	ItemID        string `json:"item_id"`
	BuyFood       bool   `json:"buy_food"`
	BuyBath       bool   `json:"buy_bath"`
	PurchaseLimit int    `json:"purchase_limit"`
}
type Social struct {
	Actions         []string `json:"actions"`
	DailyLimit      int      `json:"daily_limit"`
	IntervalSeconds int      `json:"interval_seconds"`
	FoodID          string   `json:"food_id"`
	ItemID          string   `json:"item_id"`
}
type PK struct {
	Enabled      bool   `json:"enabled"`
	DailyLimit   int    `json:"daily_limit"`
	PerOpponent  int    `json:"per_opponent"`
	Source       string `json:"source"`
	StrongerOnly bool   `json:"stronger_only"`
}
type Config struct {
	Enabled       bool   `json:"enabled"`
	Start         string `json:"start"`
	End           string `json:"end"`
	Care          Care   `json:"care"`
	Plans         []Plan `json:"plans"`
	Social        Social `json:"social"`
	PK            PK     `json:"pk"`
	Encourage     bool   `json:"encourage"`
	PauseFatigued bool   `json:"pause_fatigued"`
}

func DefaultConfig() Config {
	return Config{Start: "00:00", End: "23:59", Care: Care{Hunger: 60, Cleanliness: 60, PurchaseLimit: 3}, Plans: []Plan{}, Social: Social{Actions: []string{}, DailyLimit: 5, IntervalSeconds: 300}, PK: PK{DailyLimit: 5, PerOpponent: 3, Source: "strangers", StrongerOnly: true}, PauseFatigued: true}
}
func (c Config) Validate() error {
	for _, v := range []string{c.Start, c.End} {
		if _, e := time.Parse("15:04", v); e != nil {
			return errors.New("运行时间须为 HH:MM")
		}
	}
	if c.Care.Hunger < 0 || c.Care.Hunger > 100 || c.Care.Cleanliness < 0 || c.Care.Cleanliness > 100 {
		return errors.New("照料阈值须在 0–100 之间")
	}
	if c.Care.Bathe && c.Care.ItemID == "" {
		return errors.New("自动洗护需要选择用品 ID")
	}
	if c.Care.PurchaseLimit < 0 || c.Care.PurchaseLimit > 100 {
		return errors.New("每日自动购买次数须为 0–100")
	}
	if c.PK.DailyLimit < 0 || c.PK.DailyLimit > 100 || c.PK.PerOpponent < 1 || c.PK.PerOpponent > 3 {
		return errors.New("PK 每日上限须为 0–100，每对手上限须为 1–3")
	}
	if c.PK.Source != "friends" && c.PK.Source != "strangers" {
		return errors.New("PK 来源须为官方好友或陌生人目录")
	}
	if c.Social.DailyLimit < 0 || c.Social.DailyLimit > 100 || c.Social.IntervalSeconds < 60 || c.Social.IntervalSeconds > 86400 {
		return errors.New("互动每日上限须为 0–100，间隔须为 60–86400 秒")
	}
	seen := map[string]bool{}
	for _, a := range c.Social.Actions {
		if a != "poke_friend_pet" && a != "visit_friend_pet" && a != "feed_friend_pet" && a != "bathe_friend_pet" {
			return errors.New("未知的互动动作")
		}
		if seen[a] {
			return errors.New("互动动作重复")
		}
		seen[a] = true
		if a == "bathe_friend_pet" && c.Social.ItemID == "" {
			return errors.New("好友洗护需要用品 ID")
		}
	}
	if len(c.Plans) > 30 {
		return errors.New("最多配置 30 项计划")
	}
	seen = map[string]bool{}
	for _, p := range c.Plans {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(p.ID) || seen[p.ID] {
			return errors.New("计划 ID 无效或重复")
		}
		seen[p.ID] = true
		if p.Activity != "school" && p.Activity != "work" && p.Activity != "adventure" {
			return errors.New("无效的活动类型")
		}
		if p.Count < 1 || p.Count > 100 || len(p.Option) > 300 {
			return errors.New("计划次数须为 1–100")
		}
		if p.Stage != nil && (*p.Stage < 0 || *p.Stage > 4) {
			return errors.New("学习阶段须为 0–4")
		}
		if p.CareerType != nil && *p.CareerType < 0 {
			return errors.New("职业类型不能为负数")
		}
		if p.FriendUIN != "" && !regexp.MustCompile(`^[1-9][0-9]{0,15}$`).MatchString(p.FriendUIN) {
			return errors.New("好友 QQ 必须是正整数字符串")
		}
		if (p.FriendUIN == "") != (p.FriendPetID == "") || p.Activity == "school" && p.FriendUIN != "" {
			return errors.New("雇佣好友需要同时填写 QQ 和宠物 ID，学习不支持雇佣")
		}
	}
	return nil
}

type Task struct {
	Kind       string `json:"kind"`
	PlanID     string `json:"plan_id"`
	PetID      string `json:"pet_id"`
	StoryID    string `json:"story_id"`
	Settling   bool   `json:"settling"`
	Encouraged bool   `json:"encouraged"`
}
type Pending struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params"`
	At     string         `json:"at"`
	Reason string         `json:"reason"`
}
type State struct {
	SettledPetID   string          `json:"settled_pet_id,omitempty"`
	SettledStoryID string          `json:"settled_story_id,omitempty"`
	Date           string          `json:"date"`
	Message        string          `json:"message"`
	LastError      string          `json:"last_error"`
	UpdatedAt      string          `json:"updated_at"`
	Profile        json.RawMessage `json:"profile,omitempty"`
	Vitals         json.RawMessage `json:"vitals,omitempty"`
	Counts         map[string]int  `json:"counts"`
	LastSocial     string          `json:"last_social"`
	LastWrite      string          `json:"last_write"`
	Task           *Task           `json:"task,omitempty"`
	Pending        *Pending        `json:"pending,omitempty"`
	Failures       int             `json:"failures"`
	NextRun        string          `json:"next_run,omitempty"`
	Verify         *Verification   `json:"verify,omitempty"`
}
type Verification struct {
	Action   string  `json:"action"`
	PetID    string  `json:"pet_id"`
	ItemID   string  `json:"item_id"`
	Before   float64 `json:"before"`
	Attempts int     `json:"attempts"`
}
type Account struct {
	SelfID   int64  `json:"self_id"`
	Config   Config `json:"config"`
	State    State  `json:"state"`
	Revision int    `json:"revision"`
}
type Log struct {
	ID      int64           `json:"id"`
	SelfID  int64           `json:"self_id"`
	Action  string          `json:"action"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
	At      string          `json:"at"`
	Meta    LogMeta         `json:"meta"`
}

func decode[T any](b []byte) (T, error) {
	var v T
	e := json.Unmarshal(b, &v)
	if e != nil {
		return v, fmt.Errorf("萌卡响应格式无效：%w", e)
	}
	return v, nil
}

var zone = time.FixedZone("Asia/Shanghai", 8*3600)

func now() time.Time { return time.Now().In(zone) }
func inWindow(t time.Time, start, end string) bool {
	v := t.In(zone).Format("15:04")
	if start <= end {
		return v >= start && v <= end
	}
	return v >= start || v <= end
}
