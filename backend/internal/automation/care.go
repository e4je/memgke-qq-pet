package automation

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (e *Engine) foodStock(ctx context.Context, id int64, food string) (float64, error) {
	raw, err := e.read(ctx, id, "get_pet_food_catalog", nil)
	if err != nil {
		return 0, err
	}
	var catalog struct {
		Biscuit *float64 `json:"biscuit_count"`
		Foods   []struct {
			ID      string   `json:"food_id"`
			Balance *float64 `json:"balance"`
		} `json:"foods"`
	}
	if json.Unmarshal(raw, &catalog) != nil {
		return 0, errors.New("食物目录格式错误")
	}
	if food == "" && catalog.Biscuit != nil {
		return *catalog.Biscuit, nil
	}
	for _, f := range catalog.Foods {
		if f.ID == food && f.Balance != nil {
			return *f.Balance, nil
		}
	}
	return 0, errors.New("食物库存未知，不能当作零库存自动购买")
}
func (e *Engine) bathStock(ctx context.Context, id int64, item string) (float64, error) {
	raw, err := e.read(ctx, id, "get_pet_bath_inventory", nil)
	if err != nil {
		return 0, err
	}
	var items []struct {
		ID    string   `json:"item_id"`
		Count *float64 `json:"count"`
	}
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return 0, errors.New("洗护库存格式错误")
	}
	for _, v := range items {
		if v.ID == item {
			if v.Count == nil {
				return 0, errors.New("洗护用品库存未知")
			}
			return *v.Count, nil
		}
	}
	return 0, nil
}
func (e *Engine) care(ctx context.Context, a *Account, pet string, v vitals) (bool, error) {
	c, s := a.Config.Care, &a.State
	action := ""
	params := map[string]any{"pet_id": pet}
	var verification *Verification
	if c.Feed && v.Hunger != nil && *v.Hunger < float64(c.Hunger) && s.Counts["feed"] < 12 {
		stock, err := e.foodStock(ctx, a.SelfID, c.FoodID)
		if err != nil {
			return false, err
		}
		if stock > 0 {
			action = "feed_pet"
			if c.FoodID != "" {
				params["food_id"] = c.FoodID
			}
			verification = &Verification{Action: action, PetID: pet, Before: *v.Hunger}
		} else if c.BuyFood && c.FoodID == "" && s.Counts["purchases"] < c.PurchaseLimit {
			action = "buy_pet_food"
			params = map[string]any{"count": 1}
			verification = &Verification{Action: action, Before: stock}
		}
	}
	if action == "" && c.Bathe && v.Cleanliness != nil && *v.Cleanliness < float64(c.Cleanliness) && s.Counts["bathe"] < 12 {
		stock, err := e.bathStock(ctx, a.SelfID, c.ItemID)
		if err != nil {
			return false, err
		}
		params["item_id"] = c.ItemID
		params["count"] = 1
		if stock > 0 {
			action = "bathe_pet"
			verification = &Verification{Action: action, PetID: pet, Before: *v.Cleanliness}
		} else if c.BuyBath && s.Counts["purchases"] < c.PurchaseLimit {
			// Apply catalog minimum/default/step locally; the order API itself does not
			// validate these constraints and submitted does not mean purchase succeeded.
			raw, err := e.read(ctx, a.SelfID, "get_pet_bath_catalog", nil)
			if err != nil {
				return false, err
			}
			var items []struct {
				ID      string `json:"item_id"`
				Default int    `json:"default_count"`
				Minimum int    `json:"minimum"`
				Maximum int    `json:"maximum"`
				Step    int    `json:"step"`
			}
			if json.Unmarshal(raw, &items) != nil {
				return false, errors.New("洗护商城目录格式错误")
			}
			found := false
			for _, item := range items {
				if item.ID != c.ItemID {
					continue
				}
				count := max(1, item.Default, item.Minimum)
				if item.Step > 0 {
					count = ((count + item.Step - 1) / item.Step) * item.Step
				}
				if item.Maximum > 0 && count > item.Maximum {
					return false, errors.New("洗护目录数量限制不一致")
				}
				params["count"] = count
				found = true
				break
			}
			if !found {
				return false, errors.New("洗护用品不在官方商城目录中")
			}
			action = "buy_pet_bath_item"
			verification = &Verification{Action: action, PetID: pet, ItemID: c.ItemID, Before: stock}
		}
	}
	if action == "" {
		return false, nil
	}
	_, err := e.write(ctx, a.SelfID, s, action, params, func(json.RawMessage) error {
		s.Verify = verification
		switch action {
		case "feed_pet":
			s.Counts["feed"]++
		case "bathe_pet":
			s.Counts["bathe"]++
		default:
			s.Counts["purchases"]++
		}
		return nil
	})
	return true, err
}
func (e *Engine) verify(ctx context.Context, a *Account) error {
	s, v := &a.State, a.State.Verify
	var current float64
	var err error
	switch v.Action {
	case "buy_pet_food":
		current, err = e.foodStock(ctx, a.SelfID, "")
	case "buy_pet_bath_item":
		current, err = e.bathStock(ctx, a.SelfID, v.ItemID)
	default:
		var raw json.RawMessage
		raw, err = e.read(ctx, a.SelfID, "get_pet_vitals", map[string]any{"pet_id": v.PetID})
		if err == nil {
			var stats vitals
			stats, err = decode[vitals](raw)
			field := stats.Hunger
			if v.Action == "bathe_pet" {
				field = stats.Cleanliness
			}
			if field == nil {
				err = errors.New("效果核对缺少数值")
			} else {
				current = *field
			}
		}
	}
	if err != nil {
		return err
	}
	if current > v.Before {
		s.Verify = nil
		s.Message = "回读确认库存或数值已增加"
		return e.persist(a.SelfID, *s, &Log{Action: v.Action, Status: "verified", Message: s.Message})
	}
	v.Attempts++
	s.Message = "已提交，尚未观察到数值或库存增加；等待回读"
	if v.Attempts >= 3 {
		s.Pending = &Pending{Action: v.Action, At: now().Format(time.RFC3339), Reason: "多次回读未确认效果；已停止重复购买或使用"}
		return errors.New(s.Pending.Reason)
	}
	return nil
}
