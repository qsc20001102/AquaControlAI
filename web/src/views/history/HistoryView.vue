<script setup lang="ts">
import { ChevronDown, Download, RefreshCw, Trash2 } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import HistoryChart from "@/components/charts/HistoryChart.vue";
import { historyApi, systemApi } from "@/api/platform";

const groups = ref<any[]>([]);
const selected = ref<string[]>([]);
const expandedGroups = ref<Set<string>>(new Set());
const mode = ref<"curve" | "table">("curve");
const range = ref("1h");
const segmented = ref(true);
const interval = ref(10);
const series = ref<any[]>([]);
const table = ref<any>(null);
const cursor = ref<any[]>([]);
const retention = ref(365);
const loading = ref(false);
const treeLoading = ref(false);
const cleaning = ref(false);
const queryError = ref("");
const cache = new Map<string, any>();
const end = ref(new Date());
const start = ref(new Date(end.value.getTime() - 3600000));
let queryTimer: ReturnType<typeof setTimeout> | undefined;
let requestSerial = 0;

const archiveCount = computed(
  () =>
    groups.value
      .flatMap((g) => g.children ?? [])
      .filter((p: any) => p.lifecycle_status === "archived" && p.can_cleanup)
      .length,
);
const tableMinWidth = computed(
  () => `${Math.max(760, 170 + (table.value?.columns?.length ?? 0) * 155)}px`,
);

function pointChildren(group: any) {
  return (group.children ?? []).filter(
    (p: any) => p.type === "collection" && !p.disabled,
  );
}
function isExpanded(group: any) {
  return expandedGroups.value.has(group.id);
}
function toggleExpanded(group: any) {
  const next = new Set(expandedGroups.value);
  if (next.has(group.id)) next.delete(group.id);
  else next.add(group.id);
  expandedGroups.value = next;
}
function isGroupChecked(group: any) {
  const points = pointChildren(group);
  return (
    points.length > 0 && points.every((p: any) => selected.value.includes(p.id))
  );
}
function isGroupIndeterminate(group: any) {
  const points = pointChildren(group);
  const count = points.filter((p: any) => selected.value.includes(p.id)).length;
  return count > 0 && count < points.length;
}
function toggleGroup(group: any) {
  const ids = pointChildren(group).map((p: any) => p.id);
  const checked = isGroupChecked(group);
  if (checked) {
    selected.value = selected.value.filter((id) => !ids.includes(id));
    return;
  }
  const next = selected.value.filter((id) => !ids.includes(id));
  if (next.length + ids.length > 20) {
    alert("最多选择20个点位，请减少已选择的点位后再选择分组");
    return;
  }
  selected.value = [...next, ...ids];
}
function toggle(id: string) {
  if (selected.value.includes(id)) {
    selected.value = selected.value.filter((x) => x !== id);
    return;
  }
  if (selected.value.length >= 20) {
    alert("最多选择20个点位");
    return;
  }
  selected.value = [...selected.value, id];
}
function chooseRange(v: string) {
  range.value = v;
  const hours: Record<string, number> = {
    "1h": 1,
    "6h": 6,
    "24h": 24,
    "7d": 168,
  };
  end.value = new Date();
  start.value = new Date(end.value.getTime() - hours[v] * 3600000);
}
function inputValue(d: Date) {
  const local = new Date(d.getTime() - d.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 19);
}
function setTime(which: "start" | "end", value: string) {
  const parsed = new Date(value);
  if (!value || Number.isNaN(parsed.getTime())) return;
  range.value = "custom";
  if (which === "start") start.value = parsed;
  else end.value = parsed;
  queryError.value = end.value <= start.value ? "结束时间必须晚于开始时间" : "";
}
function syncTree(nextGroups: any[]) {
  groups.value = nextGroups;
  const points = nextGroups
    .flatMap((g: any) => g.children ?? [])
    .filter((p: any) => p.type === "collection" && !p.disabled);
  const ids = new Set(points.map((p: any) => p.id));
  const preserved = selected.value.filter((id) => ids.has(id));
  selected.value = preserved.length
    ? preserved
    : points.slice(0, 2).map((p: any) => p.id);
  const open = new Set(
    nextGroups
      .filter((g: any) => g.children?.some((p: any) => p.type === "collection"))
      .map((g: any) => g.id),
  );
  for (const id of expandedGroups.value) {
    if (nextGroups.some((g: any) => g.id === id)) open.add(id);
  }
  expandedGroups.value = open;
}
async function loadTree() {
  treeLoading.value = true;
  try {
    const r: any = await historyApi.tree();
    syncTree(r.data.tree ?? []);
  } catch (e: any) {
    queryError.value = e?.message ?? "历史点位树加载失败";
    groups.value = [];
    selected.value = [];
  } finally {
    treeLoading.value = false;
  }
}
function applySeries(data: any[]) {
  return (data ?? []).filter((s: any) =>
    (s.data ?? []).some((d: any) => d.value !== null && d.value !== undefined),
  );
}
async function query() {
  const serial = ++requestSerial;
  if (!selected.value.length) {
    series.value = [];
    table.value = null;
    cursor.value = [];
    queryError.value = "";
    loading.value = false;
    return;
  }
  if (end.value <= start.value) {
    queryError.value = "结束时间必须晚于开始时间";
    loading.value = false;
    return;
  }
  const body = {
    point_ids: [...selected.value].sort(),
    start_time: start.value.toISOString(),
    end_time: end.value.toISOString(),
  };
  const key = JSON.stringify({
    mode: mode.value,
    ...body,
    interval: interval.value,
  });
  queryError.value = "";
  loading.value = true;
  try {
    if (cache.has(key)) {
      if (mode.value === "curve") series.value = applySeries(cache.get(key));
      else table.value = cache.get(key);
      return;
    }
    if (mode.value === "curve") {
      const r: any = await historyApi.query({ ...body, max_samples: 2000 });
      const result = applySeries(r.data.series);
      cache.set(key, result);
      if (serial === requestSerial) {
        series.value = result;
        cursor.value = [];
      }
    } else {
      const r: any = await historyApi.queryTable({
        ...body,
        interval_minutes: interval.value,
      });
      cache.set(key, r.data);
      if (serial === requestSerial) table.value = r.data;
    }
  } catch (e: any) {
    if (serial === requestSerial) {
      queryError.value = e?.message ?? "历史数据查询失败";
      if (mode.value === "curve") series.value = [];
      else table.value = null;
    }
  } finally {
    if (serial === requestSerial) loading.value = false;
  }
}
async function exportCsv() {
  try {
    const r = await fetch("/api/v1/history/export", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        point_ids: selected.value,
        start_time: start.value.toISOString(),
        end_time: end.value.toISOString(),
        interval_minutes: interval.value,
      }),
    });
    if (!r.ok) throw new Error((await r.json()).message);
    const blob = await r.blob();
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = "history.csv";
    a.click();
    URL.revokeObjectURL(a.href);
  } catch (e: any) {
    alert(e?.message ?? "导出失败");
  }
}
async function cleanupArchives() {
  if (!archiveCount.value) {
    alert("当前没有可清理的已删除点位历史数据");
    return;
  }
  if (
    !confirm(
      `将永久删除 ${archiveCount.value} 个已删除点位的历史数据，删除后不可恢复，是否继续？`,
    )
  )
    return;
  cleaning.value = true;
  try {
    const r: any = await historyApi.cleanupArchives();
    cache.clear();
    await loadTree();
    alert(`已清理 ${r.data.deleted_points} 个点位的归档数据`);
  } catch (e: any) {
    alert(e?.message ?? "归档数据清理失败");
  } finally {
    cleaning.value = false;
  }
}
async function saveRetention() {
  try {
    await systemApi.setRetention(retention.value);
    alert("历史保留策略已更新");
  } catch (e: any) {
    alert(e?.message ?? "保存失败");
  }
}
function formatDateTime(value: string | Date | null | undefined) {
  if (!value) return "—";
  return new Date(value).toLocaleString("zh-CN", {
    hour12: false,
    timeZone: "Asia/Shanghai",
  });
}
function formatCursorValue(value: any) {
  return typeof value === "number" ? Number(value.toFixed(3)) : value;
}
function tableValue(value: any) {
  return value?.quality === "good" && value.value !== null ? value.value : "—";
}

watch(
  () => [
    selected.value.join(","),
    start.value.getTime(),
    end.value.getTime(),
    mode.value,
    interval.value,
  ],
  () => {
    if (queryTimer) clearTimeout(queryTimer);
    queryTimer = setTimeout(() => void query(), 180);
  },
  { immediate: true },
);
onMounted(async () => {
  try {
    const r: any = await systemApi.getRetention();
    retention.value = r.data.history_retention_days;
  } catch {
    // Retention is an auxiliary control; history remains usable if it fails.
  }
  await loadTree();
});
onBeforeUnmount(() => {
  if (queryTimer) clearTimeout(queryTimer);
});
</script>

<template>
  <section class="history-page">
    <aside class="point-tree panel">
      <div class="tree-header">
        <h3>点位选择</h3>
        <small>{{ selected.length }} / 20</small>
      </div>
      <div v-if="treeLoading && !groups.length" class="tree-empty">
        正在加载点位…
      </div>
      <div v-else-if="!groups.length" class="tree-empty">暂无历史点位</div>
      <div v-for="g in groups" v-else :key="g.id" class="tree-group">
        <div class="group-name">
          <button
            class="group-toggle"
            type="button"
            :aria-label="isExpanded(g) ? '折叠分组' : '展开分组'"
            @click="toggleExpanded(g)"
          >
            <ChevronDown :class="!isExpanded(g) && 'collapsed'" />
          </button>
          <input
            v-if="pointChildren(g).length"
            type="checkbox"
            :checked="isGroupChecked(g)"
            :indeterminate="isGroupIndeterminate(g)"
            :aria-label="`选择分组 ${g.name}`"
            @click.stop
            @change="toggleGroup(g)"
          />
          <span>{{ g.name }}</span>
          <small>{{ pointChildren(g).length }}</small>
        </div>
        <div v-show="isExpanded(g)" class="group-children">
          <label
            v-for="p in g.children"
            :key="p.id"
            :class="['point-row', p.disabled && 'disabled']"
          >
            <input
              type="checkbox"
              :disabled="p.disabled"
              :checked="selected.includes(p.id)"
              @change="toggle(p.id)"
            />
            <span>
              <b>{{ p.name }}</b>
              <small v-if="p.type === 'collection'">
                {{ p.latest_value?.value ?? "—" }} {{ p.unit }}
              </small>
            </span>
            <i v-if="p.lifecycle_status === 'archived'" class="archived">
              归档
            </i>
            <i
              v-else-if="p.latest_value"
              :class="
                p.latest_value.quality === 'good' ? 'dot good' : 'dot bad'
              "
            />
          </label>
        </div>
      </div>
    </aside>

    <div class="history-main">
      <div class="history-tools">
        <div class="quick-range">
          <button
            v-for="r in [
              ['1h', '过去1小时'],
              ['6h', '过去6小时'],
              ['24h', '过去24小时'],
              ['7d', '过去7天'],
            ]"
            :key="r[0]"
            :class="['btn', range === r[0] && 'active']"
            type="button"
            @click="chooseRange(r[0])"
          >
            {{ r[1] }}
          </button>
        </div>
        <input
          class="datetime"
          type="datetime-local"
          step="1"
          :value="inputValue(start)"
          @input="setTime('start', ($event.target as HTMLInputElement).value)"
          @change="setTime('start', ($event.target as HTMLInputElement).value)"
        />
        <span class="time-separator">~</span>
        <input
          class="datetime"
          type="datetime-local"
          step="1"
          :value="inputValue(end)"
          @input="setTime('end', ($event.target as HTMLInputElement).value)"
          @change="setTime('end', ($event.target as HTMLInputElement).value)"
        />
        <span v-if="loading" class="query-status"
          ><RefreshCw class="spin" />自动查询中</span
        >
        <span v-else-if="queryError" class="query-status error">{{
          queryError
        }}</span>
        <span v-else class="query-status">已自动同步</span>
        <button
          class="btn archive-action"
          type="button"
          :disabled="cleaning"
          @click="cleanupArchives"
        >
          <Trash2 :class="cleaning && 'spin'" />
          清理归档{{ archiveCount ? ` (${archiveCount})` : "" }}
        </button>
        <label class="retention">
          保留
          <input v-model.number="retention" type="number" min="1" max="730" />
          天
          <button class="btn" type="button" @click="saveRetention">应用</button>
        </label>
      </div>

      <div class="panel history-content">
        <div class="history-tabs">
          <button
            :class="mode === 'curve' && 'active'"
            type="button"
            @click="mode = 'curve'"
          >
            曲线
          </button>
          <button
            :class="mode === 'table' && 'active'"
            type="button"
            @click="mode = 'table'"
          >
            表格
          </button>
          <template v-if="mode === 'curve'">
            <label class="axis-toggle">
              非线性分段轴
              <input v-model="segmented" type="checkbox" />
            </label>
            <span class="trend-note">不同单位仅用于趋势对比</span>
          </template>
          <template v-else>
            <label class="interval">
              间隔
              <input
                v-model.number="interval"
                type="number"
                min="1"
                max="1440"
              />
              分钟
            </label>
            <button
              class="btn"
              type="button"
              :disabled="!selected.length"
              @click="exportCsv"
            >
              <Download />导出 CSV
            </button>
          </template>
        </div>

        <template v-if="mode === 'curve'">
          <div v-if="segmented" class="axis-banner">
            非线性分段轴 · 刻度间距不代表等比例数值差
          </div>
          <div v-if="series.length" class="curve-area">
            <HistoryChart
              :series="series"
              :segmented="segmented"
              :start-time="start.getTime()"
              :end-time="end.getTime()"
              @cursor="cursor = $event"
            />
            <div
              class="cursor-table-wrap"
              :class="cursor.length > 4 && 'cursor-scroll'"
            >
              <table class="data-table cursor-table">
                <thead>
                  <tr>
                    <th>点位名称</th>
                    <th>时间</th>
                    <th>数值</th>
                    <th>质量</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="c in cursor" :key="c.pointId ?? c.pointName">
                    <td>{{ c.pointName }}</td>
                    <td>{{ formatDateTime(c.ts) }}</td>
                    <td :class="c.quality === 'bad' ? 'quality-bad' : ''">
                      {{ c.interpolated ? "≈" : ""
                      }}{{ formatCursorValue(c.value) ?? "—" }}
                      {{ c.unit }}
                    </td>
                    <td>{{ c.quality ?? "—" }}</td>
                  </tr>
                  <tr v-if="!cursor.length">
                    <td colspan="4" class="muted-cell">
                      将鼠标移入曲线查看最近数据点
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-else class="chart-empty">所选时间范围内没有可显示的数据</div>
        </template>

        <div v-else class="table-wrap">
          <table
            class="data-table history-data-table"
            :style="{ minWidth: tableMinWidth }"
          >
            <thead>
              <tr>
                <th>时间</th>
                <th v-for="c in table?.columns ?? []" :key="c.point_id">
                  {{ c.point_name }}{{ c.unit ? `[${c.unit}]` : "" }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(t, i) in table?.time_column ?? []" :key="t">
                <td>{{ formatDateTime(t) }}</td>
                <td v-for="c in table?.columns ?? []" :key="c.point_id">
                  {{ tableValue(c.data[i]) }}
                </td>
              </tr>
              <tr v-if="!table?.time_column?.length">
                <td
                  :colspan="(table?.columns?.length ?? 0) + 1"
                  class="muted-cell"
                >
                  所选时间范围内没有表格数据
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.history-page {
  display: grid;
  grid-template-columns: 282px minmax(0, 1fr);
  gap: 14px;
  height: calc(100vh - 100px);
  min-height: 720px;
}
.point-tree {
  min-height: 0;
  overflow: auto;
}
.tree-header {
  height: 48px;
  display: flex;
  align-items: center;
  padding: 0 14px;
  border-bottom: 1px solid var(--line);
}
.tree-header h3 {
  margin: 0;
  font-size: 14px;
}
.tree-header small {
  margin-left: auto;
  color: var(--muted);
}
.tree-empty {
  padding: 28px 14px;
  color: var(--muted);
  font-size: 12px;
  text-align: center;
}
.tree-group {
  border-bottom: 1px solid var(--line);
}
.group-name {
  min-height: 42px;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 10px;
  color: #c8d7d4;
  font-size: 12px;
  background: #0b1920;
}
.group-name span {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.group-name small {
  color: var(--muted);
}
.group-toggle {
  display: inline-flex;
  width: 22px;
  height: 22px;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 0;
  background: none;
  color: var(--muted);
  cursor: pointer;
}
.group-toggle svg {
  width: 14px;
  transition: transform 0.18s ease;
}
.group-toggle svg.collapsed {
  transform: rotate(-90deg);
}
.group-name input,
.point-row input {
  width: 15px;
  height: 15px;
  accent-color: var(--green);
}
.point-row {
  min-height: 45px;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 0 13px;
  border-top: 1px solid #14262d;
  cursor: pointer;
}
.point-row:hover {
  background: #0d2027;
}
.point-row span {
  display: flex;
  flex: 1;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.point-row b {
  overflow: hidden;
  font-size: 12px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.point-row small {
  flex: 0 0 auto;
  color: var(--muted);
  font-size: 11px;
}
.point-row.disabled {
  opacity: 0.45;
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}
.dot.good {
  background: var(--green);
}
.dot.bad {
  background: var(--danger);
}
.archived {
  flex: 0 0 auto;
  padding: 2px 5px;
  border: 1px solid #72561e;
  border-radius: 3px;
  color: var(--warn);
  font-size: 10px;
  font-style: normal;
}
.history-main {
  min-width: 0;
  min-height: 0;
}
.history-tools {
  min-height: 48px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding-bottom: 8px;
}
.quick-range {
  display: flex;
}
.quick-range .btn {
  border-radius: 0;
}
.quick-range .btn:first-child {
  border-radius: 5px 0 0 5px;
}
.quick-range .btn:last-child {
  border-radius: 0 5px 5px 0;
}
.quick-range .active {
  border-color: var(--green);
  background: #10261b;
  color: var(--green);
}
.datetime {
  width: 184px;
  height: 34px;
  padding: 0 8px;
  border: 1px solid var(--line);
  border-radius: 4px;
  background: #07141a;
  color: #cfe0dc;
  color-scheme: dark;
  font-size: 11px;
}
.time-separator {
  color: var(--muted);
}
.query-status {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 11px;
  white-space: nowrap;
}
.query-status svg {
  width: 13px;
}
.query-status.error {
  color: var(--danger);
}
.archive-action {
  margin-left: auto;
}
.retention {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 11px;
}
.retention input {
  width: 62px;
  height: 30px;
  padding: 0 7px;
  border: 1px solid var(--line);
  background: #07141a;
  color: #fff;
}
.retention .btn {
  height: 30px;
  padding: 0 8px;
}
.history-content {
  display: flex;
  min-height: 0;
  height: calc(100% - 48px);
  flex-direction: column;
  overflow: hidden;
}
.history-tabs {
  min-height: 49px;
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 20px;
  padding: 0 16px;
  border-bottom: 1px solid var(--line);
}
.history-tabs > button:not(.btn) {
  height: 100%;
  border: 0;
  border-bottom: 2px solid transparent;
  background: none;
  color: #91a7ad;
  cursor: pointer;
}
.history-tabs > button.active {
  border-color: var(--green);
  color: var(--green);
}
.axis-toggle {
  margin-left: auto;
  font-size: 12px;
}
.axis-toggle input {
  accent-color: var(--green);
}
.trend-note {
  color: var(--muted);
  font-size: 11px;
}
.axis-banner {
  position: absolute;
  z-index: 2;
  margin: 54px 0 0 64px;
  padding: 4px 8px;
  border: 1px solid #295a34;
  background: #0b1b12;
  color: var(--green);
  font-size: 10px;
}
.curve-area {
  min-height: 0;
  display: flex;
  flex: 1;
  flex-direction: column;
}
.cursor-table-wrap {
  max-height: 246px;
  overflow-y: auto;
  border-top: 1px solid var(--line);
}
.cursor-table-wrap.cursor-scroll {
  overflow-y: scroll;
}
.cursor-table {
  min-width: 520px;
}
.cursor-table th,
.cursor-table td {
  height: 38px;
}
.cursor-table th {
  position: sticky;
  z-index: 1;
  top: 0;
}
.muted-cell {
  color: var(--muted);
  text-align: center;
}
.quality-bad {
  color: var(--danger);
}
.chart-empty {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  font-size: 13px;
}
.interval {
  margin-left: auto;
  color: var(--muted);
  font-size: 12px;
}
.interval input {
  width: 70px;
  height: 30px;
  padding: 0 8px;
  border: 1px solid var(--line);
  background: #07141a;
  color: #fff;
}
.table-wrap {
  min-height: 0;
  flex: 1;
  overflow: auto;
}
.history-data-table th,
.history-data-table td {
  white-space: nowrap;
}
</style>
