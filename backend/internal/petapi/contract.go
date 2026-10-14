package petapi

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

//go:embed contract.json
var contract []byte

type Field struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}
type Spec struct {
	Action string  `json:"action"`
	Write  bool    `json:"write"`
	URL    string  `json:"url"`
	Fields []Field `json:"fields"`
}

var Specs []Spec

func init() {
	if err := json.Unmarshal(contract, &Specs); err != nil {
		panic(err)
	}
}
func Find(action string) (Spec, bool) {
	for _, s := range Specs {
		if s.Action == action {
			return s, true
		}
	}
	return Spec{}, false
}
func Number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, e := x.Float64()
		return f, e == nil
	}
	return 0, false
}
func Prepare(self int64, action string, input map[string]any) (map[string]any, error) {
	spec, ok := Find(action)
	if !ok {
		return nil, fmt.Errorf("不支持的宠物 API：%s", action)
	}
	out := map[string]any{"self_id": self, "client_type": "android"}
	allowed := map[string]bool{}
	for _, field := range spec.Fields {
		allowed[field.Name] = true
		if field.Name == "self_id" || field.Name == "client_type" {
			continue
		}
		value, has := input[field.Name]
		if !has || value == nil {
			if field.Required {
				return nil, fmt.Errorf("缺少参数 %s", field.Name)
			}
			continue
		}
		if field.Type == "string" {
			v, ok := value.(string)
			if !ok || field.Required && strings.TrimSpace(v) == "" {
				return nil, fmt.Errorf("%s 必须是非空字符串", field.Name)
			}
			if len(v) > 4096 {
				return nil, fmt.Errorf("%s 过长", field.Name)
			}
		} else if field.Type == "number" {
			v, ok := Number(value)
			if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 9007199254740991 || math.Trunc(v) != v {
				return nil, fmt.Errorf("%s 必须是非负安全整数", field.Name)
			}
		}
		out[field.Name] = value
	}
	for k := range input {
		if !allowed[k] {
			return nil, fmt.Errorf("%s 不接受参数 %s", action, k)
		}
	}
	if self <= 0 {
		return nil, fmt.Errorf("无效的 QQ 账号")
	}
	if v, ok := out["count"]; ok {
		n, _ := Number(v)
		if n < 1 || (action == "bathe_pet" || action == "bathe_friend_pet") && n > 99 {
			return nil, fmt.Errorf("数量超出 API 范围")
		}
	}
	if v, ok := out["stage"]; ok {
		n, _ := Number(v)
		if n > 4 {
			return nil, fmt.Errorf("学习阶段须为 0–4")
		}
	}
	if v, ok := out["feed_type"]; ok {
		n, _ := Number(v)
		if n != 0 && n != 1001 && n != 9990032 && n != 9990033 && n != 9990034 {
			return nil, fmt.Errorf("不支持的喂食类型")
		}
	}
	if action == "start_pet_activity" {
		a := out["activity"]
		n, _ := Number(out["sub_event_type"])
		if a != "school" && a != "work" && a != "adventure" || a != "adventure" && n <= 0 {
			return nil, fmt.Errorf("活动类型或 sub_event_type 无效")
		}
	}
	if strings.Contains(action, "_pk") && out["story_id"] != nil {
		v := out["story_id"].(string)
		if !strings.HasPrefix(v, "6900_") {
			return nil, fmt.Errorf("PK story_id 必须以 6900_ 开头")
		}
	}
	return out, nil
}
func Invoke(ctx context.Context, api API, self int64, action string, input map[string]any) (json.RawMessage, error) {
	p, e := Prepare(self, action, input)
	if e != nil {
		return nil, e
	}
	return api.Call(ctx, action, p)
}
