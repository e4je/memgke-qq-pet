export async function request<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(35000),
  });
  const text = await res.text();
  let payload: { data: T; error?: string };
  try {
    payload = JSON.parse(text);
  } catch {
    throw new Error(text || `请求失败 ${res.status}`);
  }
  if (!res.ok) throw new Error(payload.error || `请求失败 ${res.status}`);
  return payload.data;
}
export const labels: Record<string, string> = {
  get_pet_profile: "本人档案",
  get_pet_vitals: "实时数值",
  get_pet_attributes: "成长属性",
  get_pet_medal_gallery: "勋章图鉴",
  get_pet_food_catalog: "食物与库存",
  feed_pet: "喂食",
  buy_pet_food: "购买默认食物",
  get_pet_bath_catalog: "洗护商城",
  get_pet_bath_inventory: "洗护库存",
  bathe_pet: "洗护",
  buy_pet_bath_item: "购买洗护用品",
  get_pet_activity_overview: "活动总览",
  get_pet_activity_options: "课程 / 岗位 / 冒险目录",
  start_pet_activity: "开始活动",
  get_pet_activity_status: "当前活动状态",
  settle_pet_activity: "结算活动",
  encourage_pet_activity: "鼓励宠物",
  get_pet_fatigue_status: "疲劳与收益",
  get_pet_pk_friends: "宠物好友目录",
  get_pet_pk_strangers: "推荐 PK 对手",
  get_pet_interaction_messages: "互动动态",
  get_pet_pk_power: "实时 PK 战力",
  poke_friend_pet: "戳一戳好友",
  get_friend_pet_profile: "好友宠物资料",
  feed_friend_pet: "给好友喂食",
  bathe_friend_pet: "给好友洗护",
  visit_friend_pet: "访问好友",
  start_pet_pk: "发起 PK",
  get_pet_pk_status: "PK 状态",
  settle_pet_pk: "结算 PK",
};
export function parameters(
  fields: { name: string; type: string; required: boolean }[],
  values: Record<string, string | number>,
): Record<string, string | number> {
  const result: Record<string, string | number> = {};
  for (const field of fields) {
    if (field.name === "self_id" || field.name === "client_type") continue;
    const v = values[field.name];
    if (v === undefined || v === "") {
      if (field.required) throw new Error(`请填写 ${field.name}`);
      continue;
    }
    if (field.type === "number") {
      const n = Number(v);
      if (!Number.isSafeInteger(n) || n < 0)
        throw new Error(`${field.name} 必须是非负整数`);
      result[field.name] = n;
    } else result[field.name] = String(v);
  }
  return result;
}
