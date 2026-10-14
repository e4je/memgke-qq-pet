<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { labels, parameters, request } from "./api";
import type { Account, Bot, Config, Info, Log } from "./types";
import AutomationForm from "./AutomationForm.vue";
const info = ref<Info>(),
  bots = ref<Bot[]>([]),
  selected = ref(0),
  account = ref<Account>(),
  draft = ref<Config>(),
  logs = ref<Log[]>([]);
const tab = ref("overview"),
  error = ref(""),
  notice = ref(""),
  busy = ref(false),
  dirty = ref(false),
  revision = ref(0);
const action = ref("get_pet_profile"),
  values = ref<Record<string, string | number>>({}),
  result = ref<unknown>(),
  recoveryStory = ref("");
const profile = ref<Record<string, unknown>>({}),
  vitals = ref<Record<string, unknown>>({});
const tabs = [
  { id: "overview", label: "宠物概览", mark: "01" },
  { id: "automation", label: "自动任务", mark: "02" },
  { id: "console", label: "宠物操作", mark: "03" },
  { id: "logs", label: "运行记录", mark: "04" },
];
const currentBot = computed(() =>
  bots.value.find(
    (b) =>
      b.self_id === selected.value &&
      (b.client_type === "android" || b.platform === "android"),
  ),
);
const available = computed(() => currentBot.value?.status === 1);
const spec = computed(() =>
  info.value?.actions.find((a) => a.action === action.value),
);
const fields = computed(
  () =>
    spec.value?.fields.filter(
      (f) => !["self_id", "client_type"].includes(f.name),
    ) || [],
);
const count = (key: string) => account.value?.state.counts?.[key] || 0;
const statusLabels: Record<string, string> = {
  submitted: "已提交",
  verified: "已核验",
  unknown: "待核对",
  error: "失败",
  confirmed: "人工确认",
};
let timer: ReturnType<typeof setInterval> | undefined;
async function attempt(fn: () => Promise<void>) {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await fn();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
async function state(id = selected.value) {
  if (!id) return;
  const data = await request<Account>(`/api/accounts/${id}`);
  if (id !== selected.value) return;
  account.value = data;
  if (data.state.profile) profile.value = data.state.profile;
  if (data.state.vitals) vitals.value = data.state.vitals;
  if (!dirty.value) {
    draft.value = structuredClone(data.config);
    revision.value = data.revision;
  }
}
async function choose(id: number) {
  if (dirty.value && !confirm("配置尚未保存，确认切换账号？")) return;
  selected.value = id;
  dirty.value = false;
  profile.value = {};
  vitals.value = {};
  values.value = {};
  result.value = undefined;
  await attempt(async () => {
    await state(id);
    if (available.value) await refreshProfile();
    await loadLogs();
  });
}
async function refreshProfile() {
  const id = selected.value;
  const p = await request<Record<string, unknown>>(
    `/api/accounts/${id}/action`,
    "POST",
    { action: "get_pet_profile", params: {} },
  );
  if (id !== selected.value) return;
  profile.value = p;
  if (p.pet_id)
    vitals.value = await request(`/api/accounts/${id}/action`, "POST", {
      action: "get_pet_vitals",
      params: { pet_id: p.pet_id },
    });
}
async function loadLogs() {
  if (selected.value)
    logs.value = await request(`/api/accounts/${selected.value}/logs`);
}
async function save() {
  if (!draft.value) return;
  await request(`/api/accounts/${selected.value}/config`, "PUT", {
    config: draft.value,
    revision: revision.value,
  });
  dirty.value = false;
  await state();
  notice.value = "配置已保存；下一轮调度自动应用。";
}
function chooseAction(name: string) {
  action.value = name;
  values.value = {};
  const targetsFriend = [
    "get_friend_pet_profile",
    "feed_friend_pet",
    "bathe_friend_pet",
    "visit_friend_pet",
  ].includes(name);
  if (profile.value.pet_id && !targetsFriend)
    values.value.pet_id = String(profile.value.pet_id);
  if (account.value?.state.task)
    values.value.story_id = account.value.state.task.story_id;
  result.value = undefined;
  tab.value = "console";
}
async function runAction() {
  if (!spec.value) return;
  const params = parameters(fields.value, values.value);
  if (
    spec.value.write &&
    !confirm(
      `确认对 QQ ${selected.value} 执行“${labels[action.value]}”？提交后不会自动重试。`,
    )
  )
    return;
  result.value = await request(
    `/api/accounts/${selected.value}/action`,
    "POST",
    { action: action.value, params },
  );
  await state();
  await loadLogs();
}
async function resolve(clearTask: boolean) {
  if (
    !confirm(
      "请确认已通过官方状态或库存接口核对结果。解除后自动化保持关闭，需要重新启用。",
    )
  )
    return;
  await request(`/api/accounts/${selected.value}/resolve`, "POST", {
    confirmed: true,
    story_id: recoveryStory.value,
    clear_task: clearTask,
  });
  dirty.value = false;
  await state();
  await loadLogs();
  notice.value = "已解除暂停。请核对配置后重新启用。";
}
async function runOnce() {
  await request(`/api/accounts/${selected.value}/run`, "POST");
  await state();
  await loadLogs();
}
async function refresh() {
  info.value = await request("/api/info");
  bots.value = await request("/api/accounts");
}
onMounted(() => {
  void attempt(async () => {
    await refresh();
    const first = bots.value.find(
      (b) => b.client_type === "android" || b.platform === "android",
    );
    if (first) {
      selected.value = first.self_id;
      await state();
      if (available.value) await refreshProfile();
      await loadLogs();
    }
  });
  timer = setInterval(() => {
    if (busy.value) return;
    void refresh()
      .then(() => state())
      .catch((e) => {
        error.value = String(e);
      });
  }, 15000);
});
onUnmounted(() => {
  if (timer) clearInterval(timer);
});
</script>

<template>
  <div class="shell">
    <aside class="sidebar">
      <a class="brand" href="#"
        ><span class="brand-icon">P</span
        ><span>宠物工作室<small>MENGKA NT · PET</small></span></a
      >
      <p class="nav-label">工作空间</p>
      <nav aria-label="主导航">
        <button
          v-for="item in tabs"
          :key="item.id"
          :class="{ active: tab === item.id }"
          :aria-current="tab === item.id ? 'page' : undefined"
          @click="
            tab = item.id;
            item.id === 'logs' && attempt(loadLogs);
          "
        >
          <span>{{ item.mark }}</span
          >{{ item.label }}
        </button>
      </nav>
      <div class="sidebar-note">
        <span class="status-dot" :class="{ online: info?.connected }"></span
        >{{ info?.connected ? "框架已连接" : "等待框架连接"
        }}<small
          >仅通过萌卡 NT 管理入口访问<br />v{{ info?.version || "2.0.0" }} ·
          {{ info?.api_count || 30 }} 项官方接口</small
        >
      </div>
    </aside>
    <main>
      <header class="topbar">
        <div>
          <span class="eyebrow"
            >QQ PET / {{ tabs.find((t) => t.id === tab)?.mark }}</span
          >
          <h1>{{ tabs.find((t) => t.id === tab)?.label }}</h1>
        </div>
        <label class="account-picker"
          >框架账号<select
            :value="selected"
            :disabled="busy"
            @change="choose(Number(($event.target as HTMLSelectElement).value))"
          >
            <option v-if="!bots.length" value="0">暂无框架账号</option>
            <option
              v-for="bot in bots"
              :key="`${bot.self_id}-${bot.client_type}`"
              :value="bot.self_id"
              :disabled="
                bot.client_type !== 'android' && bot.platform !== 'android'
              "
            >
              {{ bot.nickname || bot.self_id }} · {{ bot.self_id }} ·
              {{ bot.platform }} {{ bot.status === 1 ? "在线" : "离线" }}
            </option>
          </select></label
        >
      </header>
      <div v-if="error" class="alert danger" role="alert">
        {{ error
        }}<button aria-label="关闭错误提示" @click="error = ''">×</button>
      </div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <div v-if="!selected" class="empty">
        <h2>先在萌卡中登录 Android 账号</h2>
        <p>宠物仅支持 Android 协议，账号与登录由萌卡管理。</p>
        <button @click="attempt(refresh)">刷新账号</button>
      </div>
      <template v-else-if="account && draft">
        <div v-if="!available" class="alert">
          账号不在线，自动任务会等待恢复；配置和历史仍可查看。
        </div>
        <section v-if="account.state.pending" class="pending">
          <div>
            <span class="eyebrow">需要核对</span>
            <h2>已暂停重复操作</h2>
            <p>{{ account.state.pending.reason }}</p>
            <small>{{
              labels[account.state.pending.action] ||
              account.state.pending.action
            }}</small>
          </div>
          <div class="recovery">
            <label
              >已确认的任务 ID（恢复启动请求时填写）<input
                v-model="recoveryStory"
                placeholder="story_id" /></label
            ><button :disabled="busy" @click="attempt(() => resolve(false))">
              已核对，解除暂停</button
            ><button
              class="quiet"
              :disabled="busy"
              @click="attempt(() => resolve(true))"
            >
              已确认任务结束，清除跟踪
            </button>
          </div>
        </section>
        <template v-if="tab === 'overview'">
          <section class="hero">
            <div>
              <span class="eyebrow">一份日常，自动照料</span>
              <h2>{{ profile.pet_name || "你的 QQ 宠物" }}</h2>
              <p>照料、成长、互动与 PK，在这里有序进行。</p>
              <div class="hero-actions">
                <span class="pill">{{
                  account.config.enabled ? "自动任务已启用" : "自动任务未启用"
                }}</span
                ><button class="primary" @click="tab = 'automation'">
                  配置自动任务 →
                </button>
              </div>
            </div>
            <div class="pet-portrait">
              <img
                v-if="profile.avatar_url"
                :src="String(profile.avatar_url)"
                alt="宠物头像"
                referrerpolicy="no-referrer"
              /><span v-else>PET</span
              ><small>{{ profile.species || "QQ PET" }}</small>
            </div>
          </section>
          <div class="metrics">
            <article
              v-for="metric in [
                { key: 'hunger', label: '饱腹度' },
                { key: 'cleanliness', label: '清洁度' },
                { key: 'mood', label: '心情' },
                { key: 'gold', label: '宠物金币' },
              ]"
              :key="metric.key"
            >
              <small>{{ metric.label }}</small
              ><strong>{{ vitals[metric.key] ?? "—" }}</strong
              ><span>{{
                metric.key === "gold" ? "官方宠物余额" : "官方实时数值"
              }}</span>
            </article>
          </div>
          <div class="columns">
            <section class="card">
              <div class="card-head">
                <h2>当前任务</h2>
                <button
                  class="quiet"
                  :disabled="busy || !available"
                  @click="attempt(refreshProfile)"
                >
                  刷新资料
                </button>
              </div>
              <p class="state-message">
                {{ account.state.message || "尚未执行自动任务" }}
              </p>
              <dl>
                <dt>任务 ID</dt>
                <dd>{{ account.state.task?.story_id || "—" }}</dd>
                <dt>今日计划完成</dt>
                <dd>
                  {{
                    Object.entries(account.state.counts || {})
                      .filter(([k]) => k.startsWith("plan:"))
                      .reduce((sum, [, v]) => sum + v, 0)
                  }}
                  次
                </dd>
                <dt>今日 PK / 互动</dt>
                <dd>{{ count("pk") }} / {{ count("social") }}</dd>
                <dt>最近更新</dt>
                <dd>{{ account.state.updated_at || "—" }}</dd>
              </dl>
              <p v-if="account.state.verify" class="muted">
                正在核对 {{ labels[account.state.verify.action] }} 的实际效果。
              </p>
              <button
                v-if="account.state.task && !account.state.pending"
                class="quiet"
                :disabled="busy"
                @click="attempt(() => resolve(true))"
              >
                已核对官方状态，清除任务跟踪
              </button>
            </section>
            <section class="card">
              <h2>常用查询</h2>
              <p class="muted">通过当前框架账号调用官方 API。</p>
              <div class="quick-grid">
                <button
                  v-for="name in [
                    'get_pet_attributes',
                    'get_pet_medal_gallery',
                    'get_pet_fatigue_status',
                    'get_pet_interaction_messages',
                    'get_pet_pk_friends',
                    'get_pet_activity_status',
                  ]"
                  :key="name"
                  @click="chooseAction(name)"
                >
                  {{ labels[name] }} <span>↗</span>
                </button>
              </div>
            </section>
          </div>
        </template>
        <AutomationForm
          v-if="tab === 'automation'"
          :config="draft"
          :busy="busy"
          :dirty="dirty"
          :available="available"
          @edit="dirty = true"
          @save="attempt(save)"
          @run="attempt(runOnce)"
          @catalog="chooseAction"
        />
        <template v-if="tab === 'console'"
          ><section class="card">
            <div class="card-head">
              <div>
                <h2>官方宠物接口</h2>
                <p class="muted">
                  完整接入 30 项接口。本人和好友的 pet_id 必须与目标对应。
                </p>
              </div>
              <a
                v-if="spec"
                :href="spec.url"
                target="_blank"
                rel="noopener noreferrer"
                >官方说明 ↗</a
              >
            </div>
            <label
              >选择操作<select
                :value="action"
                @change="
                  chooseAction(($event.target as HTMLSelectElement).value)
                "
              >
                <option
                  v-for="item in info?.actions"
                  :key="item.action"
                  :value="item.action"
                >
                  {{ labels[item.action] || item.action }} ·
                  {{ item.write ? "写入" : "查询" }}
                </option>
              </select></label
            >
            <div class="parameter-grid">
              <label v-for="field in fields" :key="field.name"
                >{{ field.name }} {{ field.required ? "*" : "（可选）"
                }}<input
                  v-model="values[field.name]"
                  :type="field.type === 'number' ? 'number' : 'text'"
                  :required="field.required"
                /><small>{{ field.description }}</small></label
              >
            </div>
            <p v-if="spec?.write" class="muted">
              submitted
              只表示提交成功，不等于业务效果已核验。超时后请先查询状态。
            </p>
            <button
              class="primary"
              :disabled="busy || !available"
              @click="attempt(runAction)"
            >
              {{
                busy ? "请求中…" : spec?.write ? "提交一次操作" : "查询官方数据"
              }}
            </button>
          </section>
          <section v-if="result !== undefined" class="card">
            <h2>官方返回</h2>
            <pre>{{ JSON.stringify(result, null, 2) }}</pre>
          </section></template
        >
        <section v-if="tab === 'logs'" class="card">
          <div class="card-head">
            <div>
              <h2>最近运行记录</h2>
              <p class="muted">
                区分已提交、已核验、结果未知和失败；每账号保留最近 500 条。
              </p>
            </div>
            <button :disabled="busy" @click="attempt(loadLogs)">刷新</button>
          </div>
          <div v-if="!logs.length" class="empty compact">还没有操作记录。</div>
          <article v-for="log in logs" :key="log.id" class="log-row">
            <div>
              <span class="pill" :class="log.status">{{
                statusLabels[log.status] || log.status
              }}</span
              ><strong>{{ labels[log.action] || log.action }}</strong
              ><time>{{ log.at }}</time>
            </div>
            <p>{{ log.message }}</p>
            <details v-if="log.data">
              <summary>查看官方返回</summary>
              <pre>{{ JSON.stringify(log.data, null, 2) }}</pre>
            </details>
          </article>
        </section>
      </template>
      <footer class="page-footer">
        QQ 宠物自动化 · 萌卡 NT 插件 <span>官方 API · Android 协议</span>
      </footer>
    </main>
  </div>
</template>
