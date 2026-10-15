<script setup lang="ts">
import { ref, watch, onUnmounted } from "vue";
import { request, labels } from "./api";
import type { Log } from "./types";
const props = defineProps<{ account: number; refresh: number }>();
const logs = ref<Log[]>([]),
  status = ref(""),
  action = ref(""),
  run = ref(""),
  error = ref(""),
  busy = ref(false),
  hasMore = ref(false),
  auto = ref(true);
let seq = 0;
const statuses: Record<string, string> = {
  start: "检查开始",
  complete: "检查结束",
  info: "信息",
  skip: "跳过 / 等待",
  request: "接口请求",
  response: "接口响应",
  warning: "警告",
  error: "失败",
  unknown: "待核对",
  submitted: "已提交",
  verified: "已核验",
  confirmed: "人工确认",
};
function query() {
  const q = new URLSearchParams({ limit: "200" });
  if (status.value) q.set("status", status.value);
  if (action.value) q.set("action", action.value);
  if (run.value) q.set("run_id", run.value);
  return q;
}
async function load(older = false) {
  const n = ++seq,
    id = props.account;
  busy.value = true;
  error.value = "";
  try {
    const q = query();
    if (older && logs.value.length)
      q.set("before", String(logs.value.at(-1)!.id));
    const data = await request<Log[]>(`/api/accounts/${id}/logs?${q}`);
    if (n !== seq) return;
    logs.value = older ? [...logs.value, ...data] : data;
    hasMore.value = data.length === 200;
  } catch (e) {
    if (n === seq) error.value = String(e);
  } finally {
    if (n === seq) busy.value = false;
  }
}
watch(
  () => [props.account, status.value, action.value, run.value],
  () => {
    logs.value = [];
    void load();
  },
  { immediate: true },
);
watch(
  () => props.refresh,
  () => {
    if (auto.value && !busy.value) void load();
  },
);
const timer = setInterval(() => {
  if (auto.value && !busy.value) void load();
}, 10000);
onUnmounted(() => {
  seq++;
  clearInterval(timer);
});
</script>
<template>
  <section class="card">
    <div class="card-head">
      <div>
        <h2>完整运行日志</h2>
        <p class="muted">
          记录每轮条件、跳过原因、请求参数、响应、错误码与耗时。保留最近 7
          天，每账号最多 50,000 条。
        </p>
      </div>
      <button :disabled="busy" @click="load()">刷新</button>
    </div>
    <div class="log-filters">
      <label
        >状态<select v-model="status">
          <option value="">全部状态</option>
          <option v-for="(label, key) in statuses" :key="key" :value="key">
            {{ label }}
          </option>
        </select></label
      ><label
        >动作<select v-model="action">
          <option value="">全部动作</option>
          <option
            v-for="(label, key) in {
              ...labels,
              tick: '整轮检查',
              care: '照料判断',
              plan: '成长判断',
              schedule: '运行时段',
              configure: '保存配置',
              activity: '现有活动',
            }"
            :key="key"
            :value="key"
          >
            {{ label }}
          </option>
        </select></label
      ><label
        >单轮编号<input
          v-model.trim="run"
          placeholder="点击日志的单轮编号筛选" /></label
      ><label class="check"
        ><input v-model="auto" type="checkbox" />每 10 秒刷新</label
      ><a :href="`/api/accounts/${account}/logs/export?${query()}`" download
        >导出诊断文件</a
      >
    </div>
    <p class="muted">
      导出包含当前配置、任务状态及符合筛选条件的最近 5,000 条日志。
    </p>
    <p v-if="error" class="alert danger" role="alert">{{ error }}</p>
    <div v-if="!logs.length" class="empty compact">
      {{
        busy
          ? "正在读取日志…"
          : "暂无匹配日志。可保存配置或点击“立即检查并记录日志”。"
      }}
    </div>
    <article v-for="log in logs" :key="log.id" class="log-row">
      <div>
        <span class="pill" :class="log.status">{{
          statuses[log.status] || log.status
        }}</span
        ><strong>{{ labels[log.action] || log.action }}</strong
        ><time>{{ log.at }}</time>
      </div>
      <p>{{ log.message }}</p>
      <div v-if="log.meta?.run_id" class="log-meta">
        <button class="quiet" @click="run = log.meta.run_id">
          单轮 {{ log.meta.run_id }}
        </button>
        · {{ log.meta.source }}
        <span v-if="log.meta.duration_ms !== undefined">
          · {{ log.meta.duration_ms }} ms</span
        ><span v-if="log.meta.error_code !== undefined">
          · 错误码 {{ log.meta.error_code }}</span
        >
      </div>
      <details v-if="log.data || log.meta?.params">
        <summary>请求与响应详情</summary>
        <pre>{{
          JSON.stringify({ params: log.meta?.params, data: log.data }, null, 2)
        }}</pre>
      </details>
    </article>
    <button
      v-if="hasMore"
      :disabled="busy"
      @click="
        auto = false;
        load(true);
      "
    >
      加载更早日志（暂停自动刷新）
    </button>
  </section>
</template>
