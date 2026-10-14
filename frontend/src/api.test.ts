import { describe, it, expect } from "vitest";
import { parameters, labels } from "./api";
describe("official API forms", () => {
  it("omits empty optional fields and pins identity on the server", () => {
    expect(
      parameters(
        [
          { name: "self_id", type: "number", required: true },
          { name: "cursor", type: "string", required: false },
          { name: "count", type: "number", required: true },
        ],
        { self_id: 123, cursor: "", count: "2" },
      ),
    ).toEqual({ count: 2 });
  });
  it("does not turn a missing ID into an empty default", () => {
    expect(() =>
      parameters([{ name: "pet_id", type: "string", required: true }], {}),
    ).toThrow("pet_id");
  });
  it("rejects NaN, negative, fractional and unsafe integer inputs", () => {
    for (const count of ["abc", -1, 1.2, "999999999999999999"])
      expect(() =>
        parameters([{ name: "count", type: "number", required: true }], {
          count,
        }),
      ).toThrow();
  });
  it("has a label for every official pet action", () => {
    expect(Object.keys(labels)).toHaveLength(30);
    expect(labels.settle_pet_activity).toBe("结算活动");
  });
});
