import { describe, it, expect } from "vitest";
import { choices } from "./catalogs";
describe("official catalog selectors", () => {
  it("preserves real string IDs and displays stock, including unknown", () => {
    const c = choices("food", {
      foods: [{ food_id: "0003", name: "虾", balance: null }],
    });
    expect(c[0]?.value).toBe("0003");
    expect(c[0]?.label).toContain("库存未知");
  });
  it("binds a friend QQ and pet ID from the same directory row", () => {
    const c = choices("friend", {
      friends: [
        { user_id: "1234", pet_id: "real-pet-id", pet_name: "团团" },
        { user_id: "555", pet_name: "missing" },
      ],
    });
    expect(c).toHaveLength(1);
    expect(c[0]?.value).toBe("1234");
    expect(c[0]?.raw.pet_id).toBe("real-pet-id");
  });
  it("disables unavailable courses instead of inventing a valid option", () => {
    const c = choices("activity", {
      options: [
        {
          name: "课程 A",
          sub_event_type: 6301,
          can_do: false,
          unavailable_reason: "等级不足",
        },
        {
          name: "课程 B",
          sub_event_type: 6302,
          can_do: true,
          duration_known: false,
        },
      ],
    });
    expect(c[0]?.disabled).toBe(true);
    expect(c[0]?.hint).toBe("等级不足");
    expect(c[1]?.value).toBe("课程 B");
    expect(c[1]?.raw.sub_event_type).toBe(6302);
    expect(c[1]?.label).toContain("时长未知");
  });
  it("reads bath IDs and career types from their official records", () => {
    expect(
      choices("bath", [{ item_id: "1", name: "香皂", gold_price: 10 }])[0]
        ?.value,
    ).toBe("1");
    expect(
      choices("career", { entries: [{ career_type: 0, name: "职业零" }] })[0]
        ?.value,
    ).toBe("0");
  });
  it("keeps empty and malformed directories empty", () => {
    expect(choices("food", {})).toEqual([]);
    expect(choices("friend", null)).toEqual([]);
  });
});
