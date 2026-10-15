<script setup lang="ts">
import type { Config } from "./types";
import { labels } from "./api";
import CatalogField from "./CatalogField.vue";
const props = defineProps<{
  config: Config;
  busy: boolean;
  dirty: boolean;
  available: boolean;
  account: number;
  pet?: string;
}>();
const emit = defineEmits<{ edit: []; save: []; run: [] }>();
function addPlan() {
  props.config.plans.push({
    id: `plan-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    activity: "school",
    option: "",
    count: 1,
  });
  emit("edit");
}
function resetPlan(index: number) {
  const p = props.config.plans[index]!;
  p.option = "";
  p.stage = undefined;
  p.career_type = undefined;
  p.friend_uin = undefined;
  p.friend_pet_id = undefined;
  emit("edit");
}
</script>
<template>
  <form
    @submit.prevent="emit('save')"
    @input="emit('edit')"
    @change="emit('edit')"
  >
    <section class="card control-head">
      <div>
        <h2>自动任务总开关</h2>
        <p class="muted">
          保存后立即检查，此后每 30
          秒巡检。离线、等待和跳过原因均记录在运行日志中。
        </p>
      </div>
      <label class="toggle"
        ><input v-model="config.enabled" type="checkbox" />启用此账号</label
      >
    </section>
    <div class="columns">
      <section class="card">
        <h2>运行时间</h2>
        <div class="form-row">
          <label
            >开始<input v-model="config.start" type="time" required /></label
          ><label
            >结束<input v-model="config.end" type="time" required
          /></label>
        </div>
        <p class="muted">北京时间，支持跨午夜。已启动任务继续查询与结算。</p>
        <label class="check"
          ><input
            v-model="config.pause_fatigued"
            type="checkbox"
          />疲劳或状态未知时暂停学习 / 工作</label
        ><label class="check"
          ><input v-model="config.encourage" type="checkbox" />学习 /
          工作开始后自动鼓励一次</label
        >
      </section>
      <section class="card">
        <h2>日常照料</h2>
        <div class="form-row">
          <label class="check"
            ><input v-model="config.care.feed" type="checkbox" />自动喂食</label
          ><label
            >饱腹低于<input
              v-model.number="config.care.hunger"
              type="number"
              min="0"
              max="100"
          /></label>
        </div>
        <CatalogField
          v-model="config.care.food_id"
          :account="account"
          kind="food"
          label="使用食物"
          empty-label="默认食物（框架默认）"
          :disabled="!available"
        />
        <div class="form-row">
          <label class="check"
            ><input
              v-model="config.care.bathe"
              type="checkbox"
            />自动洗护</label
          ><label
            >清洁低于<input
              v-model.number="config.care.cleanliness"
              type="number"
              min="0"
              max="100"
          /></label>
        </div>
        <CatalogField
          v-model="config.care.item_id"
          :account="account"
          kind="bath"
          label="洗护用品"
          empty-label="请选择洗护用品"
          :disabled="!available"
        />
        <p class="muted">
          每类照料每日最多 12
          次，每次提交后核对数值。库存未知时不会购买；喂食可尝试一次并核对效果。
        </p>
      </section>
    </div>
    <section class="card">
      <div class="card-head">
        <div>
          <h2>成长计划</h2>
          <p class="muted">
            按顺序执行，结算完成后计数。直接选择官方课程、岗位或冒险项目。
          </p>
        </div>
        <button type="button" @click="addPlan">＋ 添加计划</button>
      </div>
      <div v-if="!config.plans.length" class="empty compact">
        还没有计划，可添加学习、工作或冒险。
      </div>
      <div
        v-for="(plan, index) in config.plans"
        :key="plan.id"
        class="plan-row"
      >
        <label
          >活动<select v-model="plan.activity" @change="resetPlan(index)">
            <option value="school">学习</option>
            <option value="work">工作</option>
            <option value="adventure">冒险</option>
          </select></label
        >
        <label v-if="plan.activity === 'school'"
          >学习阶段<select v-model="plan.stage" @change="plan.option = ''">
            <option :value="undefined">当前可用阶段</option>
            <option v-for="n in 5" :key="n" :value="n - 1">
              阶段 {{ n - 1 }}
            </option>
          </select></label
        >
        <CatalogField
          v-if="plan.activity === 'work'"
          :model-value="plan.career_type?.toString() || ''"
          :account="account"
          :pet="pet"
          kind="career"
          :params="{ activity: 'work' }"
          label="职业"
          empty-label="首个可用职业"
          :disabled="!available"
          @update:model-value="
            plan.career_type = $event === '' ? undefined : Number($event);
            plan.option = '';
          "
        />
        <CatalogField
          v-if="plan.activity !== 'school'"
          :model-value="plan.friend_uin || ''"
          :account="account"
          kind="friend"
          label="同行 / 雇佣好友"
          empty-label="不使用好友"
          :disabled="!available"
          @select="
            plan.friend_uin = $event?.value;
            plan.friend_pet_id = $event?.raw.pet_id as string | undefined;
            plan.option = '';
          "
        />
        <CatalogField
          v-model="plan.option"
          :account="account"
          :pet="pet"
          kind="activity"
          :params="{
            activity: plan.activity,
            ...(plan.stage !== undefined ? { stage: plan.stage } : {}),
            ...(plan.career_type !== undefined
              ? { career_type: plan.career_type }
              : {}),
            ...(plan.friend_pet_id
              ? { friend_pet_id: plan.friend_pet_id }
              : {}),
          }"
          label="成长项目"
          empty-label="自动选择首个可执行项目"
          :disabled="!available"
        />
        <label
          >每日次数<input
            v-model.number="plan.count"
            type="number"
            min="1"
            max="100" /></label
        ><button
          class="delete"
          type="button"
          :aria-label="`删除计划 ${index + 1}`"
          @click="
            config.plans.splice(index, 1);
            emit('edit');
          "
        >
          删除
        </button>
      </div>
    </section>
    <div class="columns">
      <section class="card">
        <h2>好友互动</h2>
        <div class="checks">
          <label
            v-for="name in [
              'poke_friend_pet',
              'visit_friend_pet',
              'feed_friend_pet',
              'bathe_friend_pet',
            ]"
            :key="name"
            class="check"
            ><input
              v-model="config.social.actions"
              type="checkbox"
              :value="name"
            />{{ labels[name] }}</label
          >
        </div>
        <div class="form-row">
          <label
            >每日动作上限<input
              v-model.number="config.social.daily_limit"
              type="number"
              min="0"
              max="100" /></label
          ><label
            >间隔（秒）<input
              v-model.number="config.social.interval_seconds"
              type="number"
              min="60"
              max="86400"
          /></label>
        </div>
        <CatalogField
          v-model="config.social.food_id"
          :account="account"
          kind="food"
          label="好友喂食使用食物"
          empty-label="默认食物（框架默认）"
          :disabled="!available"
        />
        <CatalogField
          v-model="config.social.item_id"
          :account="account"
          kind="bath"
          label="好友洗护用品"
          empty-label="请选择洗护用品"
          :disabled="!available"
        />
        <p class="muted">
          只使用官方宠物好友目录，每个好友的同类动作每天最多一次。
        </p>
      </section>
      <section class="card">
        <h2>自动 PK</h2>
        <label class="check"
          ><input v-model="config.pk.enabled" type="checkbox" />启用 PK</label
        ><label
          >对手来源<select v-model="config.pk.source">
            <option value="strangers">官方推荐陌生人</option>
            <option value="friends">官方宠物好友</option>
          </select></label
        >
        <div class="form-row">
          <label
            >每日发起上限<input
              v-model.number="config.pk.daily_limit"
              type="number"
              min="0"
              max="100" /></label
          ><label
            >每对手上限<input
              v-model.number="config.pk.per_opponent"
              type="number"
              min="1"
              max="3"
          /></label>
        </div>
        <label class="check"
          ><input
            v-model="config.pk.stronger_only"
            type="checkbox"
          />只挑战实时战力低于自己的宠物</label
        >
        <p class="muted">
          按官方 finished 状态推进结算，不使用硬编码名单和猜测倍率。
        </p>
      </section>
    </div>
    <section class="card">
      <h2>库存不足时补购</h2>
      <div class="form-row">
        <label class="check"
          ><input
            v-model="config.care.buy_food"
            type="checkbox"
          />允许购买默认食物</label
        ><label class="check"
          ><input
            v-model="config.care.buy_bath"
            type="checkbox"
          />允许购买指定洗护用品</label
        ><label
          >每日购买请求上限<input
            v-model.number="config.care.purchase_limit"
            type="number"
            min="0"
            max="100"
        /></label>
      </div>
      <p class="muted">
        消耗宠物金币，默认关闭。未知库存不下单；每次购买后先核对库存，未确认效果不再购买。指定非默认食物不自动补购。
      </p>
    </section>
    <footer class="save-bar">
      <span>{{ dirty ? "有尚未保存的修改" : "配置已同步" }}</span>
      <div>
        <button
          type="button"
          :disabled="busy || dirty || !available"
          @click="emit('run')"
        >
          立即检查并记录日志</button
        ><button class="primary" type="submit" :disabled="busy">
          {{ busy ? "处理中…" : "保存并应用自动任务" }}
        </button>
      </div>
    </footer>
  </form>
</template>
