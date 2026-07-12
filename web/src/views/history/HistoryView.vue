<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Download, Search } from "lucide-vue-next";
import HistoryChart from "@/components/charts/HistoryChart.vue";
import { historyApi, systemApi } from "@/api/platform";
const groups = ref<any[]>([]),
  selected = ref<string[]>([]),
  mode = ref<"curve" | "table">("curve"),
  range = ref("1h"),
  segmented = ref(true),
  interval = ref(10),
  series = ref<any[]>([]),
  table = ref<any>(null),
  cursor = ref<any[]>([]),
  retention = ref(365),
  cache = new Map<string, any>();
const end = ref(new Date()),
  start = ref(new Date(Date.now() - 3600000));
const selectedPoints = computed(() =>
  groups.value
    .flatMap((g) => g.children ?? [])
    .filter((p: any) => selected.value.includes(p.id)),
);
function chooseRange(v: string) {
  range.value = v;
  const hours: { [k: string]: number } = {
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
  if (which === "start") start.value = parsed;
  else end.value = parsed;
  range.value = "custom";
}
function toggle(id: string) {
  const i = selected.value.indexOf(id);
  if (i >= 0) selected.value.splice(i, 1);
  else if (selected.value.length < 20) selected.value.push(id);
  else alert("最多选择20个点位");
}
async function loadTree() {
  try {
    const r: any = await historyApi.tree();
    groups.value = r.data.tree;
    const first = groups.value
      .flatMap((g) => g.children ?? [])
      .filter((p: any) => p.type === "collection")
      .slice(0, 2);
    selected.value = first.map((p: any) => p.id);
  } catch {
    groups.value = [];
    return;
  }
  try {
    await query();
  } catch {
    series.value = [];
  }
}
async function query() {
  if (!selected.value.length) return;
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
  if (cache.has(key)) {
    mode.value === "curve"
      ? (series.value = cache.get(key))
      : (table.value = cache.get(key));
    return;
  }
  if (mode.value === "curve") {
    const r: any = await historyApi.query({ ...body, max_samples: 2000 });
    series.value = r.data.series;
    cache.set(key, series.value);
  } else {
    const r: any = await historyApi.queryTable({
      ...body,
      interval_minutes: interval.value,
    });
    table.value = r.data;
    cache.set(key, table.value);
  }
}
async function switchMode(v: "curve" | "table") {
  mode.value = v;
  await query();
}
function exportCsv() {
  fetch("/api/v1/history/export", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      point_ids: selected.value,
      start_time: start.value.toISOString(),
      end_time: end.value.toISOString(),
      interval_minutes: interval.value,
    }),
  }).then(async (r) => {
    if (!r.ok) throw new Error((await r.json()).message);
    const blob = await r.blob(),
      a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = "history.csv";
    a.click();
    URL.revokeObjectURL(a.href);
  });
}
async function saveRetention() {
  await systemApi.setRetention(retention.value);
  alert("历史保留策略已更新");
}
onMounted(async () => {
  try {
    const r: any = await systemApi.getRetention();
    retention.value = r.data.history_retention_days;
  } catch {}
  await loadTree();
});
</script>
<template>
  <section class="history-page">
    <aside class="point-tree panel">
      <h3>
        点位选择 <small>{{ selected.length }} / 20</small>
      </h3>
      <div v-for="g in groups" :key="g.id" class="tree-group">
        <div class="group-name">⌄ {{ g.name }}</div>
        <label
          v-for="p in g.children"
          :key="p.id"
          :class="['point-row', p.disabled && 'disabled']"
          ><input
            type="checkbox"
            :disabled="p.disabled"
            :checked="selected.includes(p.id)"
            @change="toggle(p.id)" /><span
            ><b>{{ p.name }}</b
            ><small v-if="p.type === 'collection'"
              >{{ p.latest_value?.value ?? "—" }} {{ p.unit }}</small
            ></span
          ><i v-if="p.lifecycle_status === 'archived'" class="archived">归档</i
          ><i
            v-else-if="p.latest_value"
            :class="p.latest_value.quality === 'good' ? 'dot good' : 'dot bad'"
        /></label>
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
          @change="setTime('start', ($event.target as HTMLInputElement).value)"
        />
        <span class="time-separator">~</span>
        <input
          class="datetime"
          type="datetime-local"
          step="1"
          :value="inputValue(end)"
          @change="setTime('end', ($event.target as HTMLInputElement).value)"
        />
        <button class="btn btn-primary" @click="query"><Search />查询</button>
        <label class="retention"
          >保留
          <input v-model.number="retention" type="number" min="1" max="730" />
          天 <button class="btn" @click="saveRetention">应用</button></label
        >
      </div>
      <div class="panel history-content">
        <div class="history-tabs">
          <button
            :class="mode === 'curve' && 'active'"
            @click="switchMode('curve')"
          >
            曲线</button
          ><button
            :class="mode === 'table' && 'active'"
            @click="switchMode('table')"
          >
            表格</button
          ><template v-if="mode === 'curve'"
            ><label class="axis-toggle"
              >非线性分段轴 <input v-model="segmented" type="checkbox" /></label
            ><span class="trend-note">不同单位仅用于趋势对比</span></template
          ><template v-else
            ><label class="interval"
              >间隔
              <input
                v-model.number="interval"
                type="number"
                min="1"
                max="1440"
              />
              分钟</label
            ><button class="btn" @click="exportCsv">
              <Download />导出CSV
            </button></template
          >
        </div>
        <template v-if="mode === 'curve'"
          ><div v-if="segmented" class="axis-banner">
            非线性分段轴 · 刻度间距不代表等比例数值差
          </div>
          <HistoryChart
            :series="series"
            :segmented="segmented"
            @cursor="cursor = $event"
          />
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
              <tr v-for="c in cursor" :key="c.pointName">
                <td>{{ c.pointName }}</td>
                <td>
                  {{
                    c.ts
                      ? new Date(c.ts).toLocaleTimeString("zh-CN", {
                          hour12: false,
                        })
                      : "—"
                  }}
                </td>
                <td :class="c.quality === 'bad' ? 'quality-bad' : ''">
                  {{ c.value ?? "—" }} {{ c.unit }}
                </td>
                <td>{{ c.quality ?? "—" }}</td>
              </tr>
              <tr v-if="!cursor.length">
                <td colspan="4" class="muted-cell">
                  将鼠标移入曲线查看最近数据点
                </td>
              </tr>
            </tbody>
          </table></template
        >
        <div v-else class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>时间</th>
                <th v-for="c in table?.columns" :key="c.point_id">
                  {{ c.point_name }}{{ c.unit ? `[${c.unit}]` : "" }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(t, i) in table?.time_column" :key="t">
                <td>
                  {{ new Date(t).toLocaleString("zh-CN", { hour12: false }) }}
                </td>
                <td v-for="c in table.columns" :key="c.point_id">
                  {{ c.data[i]?.quality === "good" ? c.data[i].value : "—" }}
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
  grid-template-columns: 270px 1fr;
  gap: 14px;
  height: calc(100vh - 100px);
}
.point-tree {
  overflow: auto;
}
.point-tree h3 {
  height: 48px;
  margin: 0;
  padding: 0 14px;
  display: flex;
  align-items: center;
  border-bottom: 1px solid var(--line);
  font-size: 14px;
}
.point-tree h3 small {
  margin-left: auto;
  color: var(--muted);
}
.tree-group {
  border-bottom: 1px solid var(--line);
}
.group-name {
  padding: 12px 14px;
  color: #c8d7d4;
  font-size: 12px;
}
.point-row {
  height: 45px;
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
.point-row input {
  accent-color: var(--green);
}
.point-row span {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: space-between;
  min-width: 0;
}
.point-row b {
  font-size: 12px;
  font-weight: 500;
}
.point-row small {
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
  font-size: 10px;
  color: var(--warn);
  font-style: normal;
}
.history-main {
  min-width: 0;
}
.history-tools {
  height: 48px;
  display: flex;
  gap: 8px;
  align-items: center;
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
  color: var(--green);
  border-color: var(--green);
  background: #10261b;
}
.history-tools > .btn {
  margin-left: auto;
}
.datetime {
  height: 34px;
  width: 184px;
  background: #07141a;
  border: 1px solid var(--line);
  border-radius: 4px;
  color: #cfe0dc;
  padding: 0 8px;
  font-size: 11px;
  color-scheme: dark;
}
.time-separator {
  color: var(--muted);
}
.retention {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--muted);
}
.retention input {
  width: 62px;
  height: 30px;
  background: #07141a;
  border: 1px solid var(--line);
  color: #fff;
  padding: 0 7px;
}
.retention .btn {
  height: 30px;
  padding: 0 8px;
}
.history-content {
  overflow: hidden;
}
.history-tabs {
  height: 49px;
  display: flex;
  align-items: center;
  border-bottom: 1px solid var(--line);
  padding: 0 16px;
  gap: 20px;
}
.history-tabs > button:not(.btn) {
  height: 100%;
  border: 0;
  background: none;
  color: #91a7ad;
  border-bottom: 2px solid transparent;
}
.history-tabs > button.active {
  color: var(--green);
  border-color: var(--green);
}
.axis-toggle {
  margin-left: auto;
  font-size: 12px;
}
.axis-toggle input {
  accent-color: var(--green);
}
.trend-note {
  font-size: 11px;
  color: var(--muted);
}
.axis-banner {
  position: absolute;
  z-index: 2;
  margin: 54px 0 0 64px;
  color: var(--green);
  font-size: 10px;
  border: 1px solid #295a34;
  background: #0b1b12;
  padding: 4px 8px;
}
.cursor-table {
  border-top: 1px solid var(--line);
}
.cursor-table th,
.cursor-table td {
  height: 38px;
}
.muted-cell {
  color: var(--muted);
  text-align: center;
}
.interval {
  margin-left: auto;
  font-size: 12px;
  color: var(--muted);
}
.interval input {
  width: 70px;
  height: 30px;
  background: #07141a;
  border: 1px solid var(--line);
  color: #fff;
  padding: 0 8px;
}
.table-wrap {
  max-height: calc(100vh - 220px);
  overflow: auto;
}
</style>
