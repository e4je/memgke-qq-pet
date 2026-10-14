<script setup lang="ts">
import type { Config } from "./types";
import { labels } from "./api";
const props = defineProps<{
  config: Config;
  busy: boolean;
  dirty: boolean;
  available: boolean;
}>();
const emit = defineEmits<{
  edit: [];
  save: [];
  run: [];
  catalog: [action: string];
}>();
function addPlan() {
  props.config.plans.push({
    id: `plan-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    activity: "school",
    option: "",
    count: 1,
  });
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
          每 30 秒检查一次，离线自动等待；未确认的写入会暂停。
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
        <label
          >食物 ID<input
            v-model="config.care.food_id"
            placeholder="留空使用默认食物"
        /></label>
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
        <label
          >洗护用品 ID<input
            v-model="config.care.item_id"
            placeholder="使用官方目录中的 ID"
        /></label>
        <p class="muted">每类照料每日最多 12 次，每次提交后核对数值。</p>
      </section>
    </div>
    <section class="card">
      <div class="card-head">
        <div>
          <h2>成长计划</h2>
          <p class="muted">
            按顺序执行，完成并结算后计数；项目名使用官方目录原名。
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
          >活动<select
            v-model="plan.activity"
            @change="
              plan.stage = undefined;
              plan.career_type = undefined;
              plan.friend_uin = undefined;
              plan.friend_pet_id = undefined;
            "
          >
            <option value="school">学习</option>
            <option value="work">工作</option>
            <option value="adventure">冒险</option>
          </select></label
        ><label
          >项目原名<input
            v-model="plan.option"
            placeholder="留空选首个可执行项目" /></label
        ><label
          >每日次数<input
            v-model.number="plan.count"
            type="number"
            min="1"
            max="100" /></label
        ><label v-if="plan.activity === 'school'"
          >阶段<select v-model="plan.stage">
            <option :value="undefined">自动</option>
            <option v-for="n in 5" :key="n" :value="n - 1">{{ n - 1 }}</option>
          </select></label
        ><label v-if="plan.activity === 'work'"
          >职业类型<input
            :value="plan.career_type"
            @input="
              plan.career_type =
                ($event.target as HTMLInputElement).value === ''
                  ? undefined
                  : Number(($event.target as HTMLInputElement).value)
            "
            type="number"
            min="0"
            placeholder="可选" /></label
        ><label v-if="plan.activity !== 'school'"
          >好友 QQ（可选）<input
            v-model="plan.friend_uin"
            inputmode="numeric" /></label
        ><label v-if="plan.activity !== 'school'"
          >好友宠物 ID（配对填写）<input v-model="plan.friend_pet_id" /></label
        ><button
          class="quiet"
          type="button"
          @click="emit('catalog', 'get_pet_activity_options')"
        >
          查看目录</button
        ><button
          class="delete"
          type="button"
          :aria-label="`删除计划 ${index + 1}`"
          @click="
            config.plans.splice(index, 1);
            emit('edit');
          "
        >
          ×
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
        <label
          >好友喂食 ID<input
            v-model="config.social.food_id"
            placeholder="默认食物可留空" /></label
        ><label>好友洗护用品 ID<input v-model="config.social.item_id" /></label>
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
          立即检查一次</button
        ><button class="primary" type="submit" :disabled="busy">
          {{ busy ? "处理中…" : "保存自动任务" }}
        </button>
      </div>
    </footer>
  </form>
</template>
