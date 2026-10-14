export interface Plan {
  id: string;
  activity: string;
  option: string;
  count: number;
  stage?: number;
  career_type?: number;
  friend_uin?: string;
  friend_pet_id?: string;
}
export interface Config {
  enabled: boolean;
  start: string;
  end: string;
  encourage: boolean;
  pause_fatigued: boolean;
  care: {
    feed: boolean;
    hunger: number;
    food_id: string;
    bathe: boolean;
    cleanliness: number;
    item_id: string;
    buy_food: boolean;
    buy_bath: boolean;
    purchase_limit: number;
  };
  plans: Plan[];
  social: {
    actions: string[];
    daily_limit: number;
    interval_seconds: number;
    food_id: string;
    item_id: string;
  };
  pk: {
    enabled: boolean;
    daily_limit: number;
    per_opponent: number;
    source: string;
    stronger_only: boolean;
  };
}
export interface State {
  message: string;
  last_error: string;
  updated_at: string;
  date: string;
  profile?: Record<string, unknown>;
  vitals?: Record<string, unknown>;
  counts: Record<string, number>;
  task?: { kind: string; story_id: string; settling: boolean };
  pending?: { action: string; reason: string };
  verify?: { action: string; attempts: number };
  next_run?: string;
}
export interface Account {
  self_id: number;
  config: Config;
  state: State;
  revision: number;
}
export interface Bot {
  self_id: number;
  nickname: string;
  client_type: string;
  platform: string;
  status: number;
}
export interface Field {
  name: string;
  type: string;
  required: boolean;
  description: string;
}
export interface Spec {
  action: string;
  write: boolean;
  url: string;
  fields: Field[];
}
export interface Info {
  version: string;
  connected: boolean;
  api_count: number;
  actions: Spec[];
}
export interface Log {
  id: number;
  action: string;
  status: string;
  message: string;
  data: unknown;
  at: string;
}
