// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import CatalogField from "./CatalogField.vue";
import { request } from "./api";
vi.mock("./api", () => ({ request: vi.fn() }));
const call = vi.mocked(request);
afterEach(() => {
  vi.clearAllMocks();
});
describe("catalog selection interactions", () => {
  it("shows names and emits the selected real ID", async () => {
    call.mockResolvedValue({
      foods: [{ food_id: "0003", name: "虾", balance: 5 }],
    });
    const w = mount(CatalogField, {
      props: { account: 123, kind: "food", label: "食物", modelValue: "" },
    });
    await flushPromises();
    expect(w.text()).toContain("虾 · 库存 5");
    await w.get("select").setValue("0003");
    expect(w.emitted("update:modelValue")?.[0]).toEqual(["0003"]);
    expect(call).toHaveBeenCalledWith("/api/accounts/123/action", "POST", {
      action: "get_pet_food_catalog",
      params: {},
    });
    w.unmount();
  });
  it("emits both friend identifiers from the same row and loads the next page", async () => {
    call
      .mockResolvedValueOnce({
        friends: [{ user_id: "456", pet_id: "pet-456", pet_name: "团团" }],
        has_more: true,
        next_cursor: "page-2",
      })
      .mockResolvedValueOnce({
        friends: [{ user_id: "789", pet_id: "pet-789", pet_name: "圆圆" }],
        has_more: false,
      });
    const w = mount(CatalogField, {
      props: { account: 123, kind: "friend", label: "好友" },
    });
    await flushPromises();
    await w.get("select").setValue("456");
    const selected = w.emitted("select")?.[0]?.[0] as {
      value: string;
      raw: Record<string, unknown>;
    };
    expect(selected.value).toBe("456");
    expect(selected.raw.pet_id).toBe("pet-456");
    await w
      .findAll("button")
      .find((b) => b.text().includes("加载更多"))!
      .trigger("click");
    await flushPromises();
    expect(w.text()).toContain("圆圆");
    expect(call.mock.calls.at(-1)?.[2]).toEqual({
      action: "get_pet_pk_friends",
      params: { cursor: "page-2" },
    });
    w.unmount();
  });
  it("reloads the course list with selected stage and disables unavailable options", async () => {
    call.mockResolvedValue({
      options: [
        {
          name: "课程 A",
          sub_event_type: 6301,
          can_do: false,
          unavailable_reason: "等级不足",
        },
      ],
    });
    const w = mount(CatalogField, {
      props: {
        account: 123,
        pet: "real-pet",
        kind: "activity",
        label: "项目",
        params: { activity: "school", stage: 1 },
      },
    });
    await flushPromises();
    expect(
      w
        .findAll("option")
        .find((x) => x.text().includes("课程 A"))
        ?.attributes("disabled"),
    ).toBeDefined();
    await w.setProps({ params: { activity: "school", stage: 2 } });
    await flushPromises();
    expect(call.mock.calls.at(-1)?.[2]).toEqual({
      action: "get_pet_activity_options",
      params: { activity: "school", stage: 2, pet_id: "real-pet" },
    });
    w.unmount();
  });
  it("does not let a late response from an old account replace current choices", async () => {
    let finish: (x: unknown) => void = () => {};
    call
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      )
      .mockResolvedValueOnce({
        foods: [{ food_id: "new", name: "新账号食物", balance: 1 }],
      });
    const w = mount(CatalogField, {
      props: { account: 123, kind: "food", label: "食物" },
    });
    await w.setProps({ account: 456 });
    await flushPromises();
    finish({ foods: [{ food_id: "old", name: "旧账号食物" }] });
    await flushPromises();
    expect(w.text()).toContain("新账号食物");
    expect(w.text()).not.toContain("旧账号食物");
    w.unmount();
  });
  it("preserves saved values when a directory fails instead of silently changing defaults", async () => {
    call.mockRejectedValue(new Error("directory unavailable"));
    const w = mount(CatalogField, {
      props: {
        account: 123,
        kind: "bath",
        label: "洗护",
        modelValue: "saved-id",
      },
    });
    await flushPromises();
    expect(w.text()).toContain("directory unavailable");
    expect(w.text()).toContain("原配置未在当前目录");
    expect(w.emitted("update:modelValue")).toBeUndefined();
    w.unmount();
  });
});
