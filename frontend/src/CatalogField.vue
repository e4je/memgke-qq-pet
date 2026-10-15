<script setup lang="ts">
import { ref, watch, computed } from "vue";
import { request } from "./api";
import { choices, type CatalogKind, type Choice } from "./catalogs";
const props = defineProps<{
  account: number;
  pet?: string;
  kind: CatalogKind;
  params?: Record<string, unknown>;
  modelValue?: string;
  label: string;
  emptyLabel?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  select: [choice: Choice | undefined];
}>();
const items = ref<Choice[]>([]),
  loading = ref(false),
  error = ref(""),
  cursor = ref(""),
  more = ref(false);
let generation = 0;
const selected = computed(() =>
  items.value.find((x) => x.value === props.modelValue),
);
const missing = computed(
  () =>
    !!props.modelValue &&
    !items.value.some((x) => x.value === props.modelValue),
);
async function load(append = false) {
  const seq = ++generation;
  loading.value = true;
  error.value = "";
  if (!append) {
    items.value = [];
    cursor.value = "";
    more.value = false;
  }
  try {
    if (!props.account || props.disabled)
      throw Error("账号离线，请恢复登录后刷新目录");
    const actions = {
      food: "get_pet_food_catalog",
      bath: "get_pet_bath_catalog",
      friend: "get_pet_pk_friends",
      career: "get_pet_activity_overview",
      activity: "get_pet_activity_options",
    };
    const params: Record<string, unknown> = { ...props.params };
    if (props.kind === "career" || props.kind === "activity") {
      if (!props.pet) throw Error("尚未读取本人宠物资料，请刷新资料");
      params.pet_id = props.pet;
    }
    if (
      props.kind === "activity" &&
      params.activity === "school" &&
      params.stage === undefined
    ) {
      const overview = await request<{ current_stage?: number }>(
        `/api/accounts/${props.account}/action`,
        "POST",
        {
          action: "get_pet_activity_overview",
          params: { pet_id: props.pet, activity: "school" },
        },
      );
      if (seq !== generation) return;
      if (
        typeof overview.current_stage !== "number" ||
        !Number.isInteger(overview.current_stage) ||
        overview.current_stage < 0 ||
        overview.current_stage > 4
      )
        throw Error("当前学习阶段未知，请先选择学习阶段");
      params.stage = overview.current_stage;
    }
    if (append && cursor.value) params.cursor = cursor.value;
    const result = await request<unknown>(
      `/api/accounts/${props.account}/action`,
      "POST",
      { action: actions[props.kind], params },
    );
    if (seq !== generation) return;
    const next = choices(props.kind, result);
    items.value = append
      ? [
          ...items.value,
          ...next.filter((n) => !items.value.some((i) => i.value === n.value)),
        ]
      : next;
    const page = (result || {}) as Record<string, unknown>;
    cursor.value = String(page.next_cursor || "");
    more.value = page.has_more === true && !!cursor.value;
    if (!items.value.length)
      error.value = "官方目录当前没有可用条目；可稍后刷新。";
  } catch (e) {
    if (seq === generation)
      error.value = e instanceof Error ? e.message : String(e);
  } finally {
    if (seq === generation) loading.value = false;
  }
}
function change(value: string) {
  emit("update:modelValue", value);
  emit(
    "select",
    items.value.find((x) => x.value === value),
  );
}
watch(
  () =>
    JSON.stringify([
      props.account,
      props.pet,
      props.kind,
      props.params,
      props.disabled,
    ]),
  () => {
    void load();
  },
  { immediate: true },
);
</script>
<template>
  <label class="catalog-field"
    ><span>{{ label }}</span>
    <div class="catalog-controls">
      <select
        :value="modelValue || ''"
        :disabled="disabled || loading"
        @change="change(($event.target as HTMLSelectElement).value)"
      >
        <option value="">
          {{ loading ? "正在读取官方目录…" : emptyLabel || "请选择" }}
        </option>
        <option v-if="missing" :value="modelValue" disabled>
          原配置未在当前目录中找到，请重新选择
        </option>
        <option
          v-for="item in items"
          :key="item.value"
          :value="item.value"
          :disabled="item.disabled"
        >
          {{ item.label }}{{ item.disabled ? "（暂不可执行）" : "" }}
        </option></select
      ><button
        type="button"
        :disabled="disabled || loading"
        :aria-label="`刷新${label}`"
        @click="load()"
      >
        刷新
      </button>
    </div>
    <small v-if="error" class="catalog-error" role="status">{{ error }}</small
    ><small v-else-if="selected?.hint" class="muted">{{ selected.hint }}</small
    ><button v-if="more" type="button" :disabled="loading" @click="load(true)">
      加载更多好友
    </button></label
  >
</template>
