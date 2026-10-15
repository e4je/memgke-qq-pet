export type CatalogKind = "food" | "bath" | "friend" | "career" | "activity";
export interface Choice {
  value: string;
  label: string;
  disabled?: boolean;
  hint?: string;
  raw: Record<string, unknown>;
}
type Row = Record<string, unknown>;
const rows = (x: unknown): Row[] =>
  Array.isArray(x) ? x.filter((v) => v && typeof v === "object") : [];
export function choices(kind: CatalogKind, payload: unknown): Choice[] {
  const data = (payload || {}) as Row;
  const list =
    kind === "food"
      ? rows(data.foods)
      : kind === "bath"
        ? rows(payload)
        : kind === "friend"
          ? rows(data.friends)
          : kind === "career"
            ? rows(data.entries)
            : rows(data.options);
  const result: Choice[] = [];
  const seen = new Set<string>();
  for (const row of list) {
    const id =
      kind === "food"
        ? row.food_id
        : kind === "bath"
          ? row.item_id
          : kind === "friend"
            ? row.user_id
            : kind === "career"
              ? (row.career_type ?? row.type)
              : row.name;
    if (
      id === undefined ||
      id === null ||
      String(id) === "" ||
      seen.has(String(id))
    )
      continue;
    if (kind === "friend" && !row.pet_id) continue;
    seen.add(String(id));
    const name = String(row.name ?? row.pet_name ?? row.nickname ?? id);
    let extra = "";
    if (kind === "food")
      extra =
        row.balance === null || row.balance === undefined
          ? "库存未知"
          : `库存 ${row.balance}`;
    if (kind === "bath")
      extra = row.gold_price === undefined ? "" : `${row.gold_price} 金币`;
    if (kind === "friend") extra = String(row.user_id);
    if (kind === "activity")
      extra = String(
        row.duration_text ??
          (row.duration_known ? `${row.duration_seconds} 秒` : "时长未知"),
      );
    const hint = String(
      row.unavailable_reason || row.warning || row.description || "",
    );
    result.push({
      value: String(id),
      label: name + (extra ? ` · ${extra}` : ""),
      hint,
      disabled: kind === "activity" && row.can_do !== true,
      raw: row,
    });
  }
  return result;
}
