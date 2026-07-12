<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { Download, Plus, Upload, ShieldAlert } from "lucide-vue-next";
import {
  writePointApi,
  deviceApi,
  exportConfig,
  importConfig,
} from "@/api/platform";
const items = ref<any[]>([]),
  logs = ref<any[]>([]),
  tab = ref<"points" | "logs">("points"),
  devices = ref<any[]>([]),
  show = ref(false),
  editing = ref<string | null>(null);
const form = reactive({
  name: "",
  group_name: "default",
  device_id: "",
  enabled: true,
  write_enabled: false,
  address: "",
  data_type: "REAL",
  unit: "",
  readback_tolerance: 0.0001,
});
async function load() {
  try {
    const [a, b]: any = await Promise.all([
      writePointApi.list({ page_size: 100 }),
      writePointApi.logs({ page_size: 100 }),
    ]);
    items.value = a.data.items;
    logs.value = b.data.items;
  } catch {
    items.value = [];
  }
}
async function execute(p: any) {
  const raw = prompt(`向 ${p.name} (${p.address}) 写入值：`);
  if (raw === null) return;
  const value = p.data_type === "BOOL" ? raw === "true" : Number(raw);
  const reason = prompt("写入原因（可选）：") ?? "";
  await writePointApi.write(p.id, { value, reason });
  alert("写入及回读验证成功");
  await load();
}
function open(p?: any) {
  editing.value = p?.id ?? null;
  Object.assign(
    form,
    p ?? {
      name: "",
      group_name: "default",
      device_id: devices.value[0]?.id ?? "",
      enabled: true,
      write_enabled: false,
      address: "",
      data_type: "REAL",
      unit: "",
      readback_tolerance: 0.0001,
    },
  );
  show.value = true;
}
async function save() {
  editing.value
    ? await writePointApi.update(editing.value, form)
    : await writePointApi.create(form);
  show.value = false;
  await load();
}
async function remove(id: string) {
  if (confirm("确认删除该写入点？")) {
    await writePointApi.remove(id);
    await load();
  }
}
async function upload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0];
  if (!file) return;
  const r: any = await importConfig("write-points", file);
  alert(
    `导入完成：新增${r.data.created}，更新${r.data.updated}，失败${r.data.failed}`,
  );
  await load();
}
onMounted(async () => {
  const r: any = await deviceApi.list({ page_size: 100 });
  devices.value = r.data.items;
  await load();
});
</script>
<template>
  <section>
    <div class="page-head">
      <div>
        <h2>数据写入</h2>
        <p>人工写入、回读验证与完整操作审计</p>
      </div>
      <div class="toolbar">
        <label class="btn"
          ><Upload />导入CSV<input
            hidden
            type="file"
            accept=".csv"
            @change="upload"
        /></label>
        <button class="btn" @click="exportConfig('write-points')">
          <Download />导出CSV
        </button>
        <button class="btn btn-primary" @click="open()">
          <Plus />新增写入点
        </button>
      </div>
    </div>
    <div class="toolbar" style="margin-bottom: 12px">
      <button
        :class="['btn', tab === 'points' && 'btn-primary']"
        @click="tab = 'points'"
      >
        写入点</button
      ><button
        :class="['btn', tab === 'logs' && 'btn-primary']"
        @click="tab = 'logs'"
      >
        操作日志</button
      ><span style="margin-left: auto; color: var(--warn); font-size: 12px"
        ><ShieldAlert style="width: 15px; vertical-align: middle" />
        写入必须启用并通过回读验证</span
      >
    </div>
    <div class="panel">
      <table v-if="tab === 'points'" class="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>分组</th>
            <th>设备</th>
            <th>地址 / 类型</th>
            <th>单位</th>
            <th>回读容差</th>
            <th>允许写入</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in items" :key="p.id">
            <td>{{ p.name }}</td>
            <td>{{ p.group_name }}</td>
            <td>{{ p.device_name }}</td>
            <td>
              {{ p.address }} <span class="tag">{{ p.data_type }}</span>
            </td>
            <td>{{ p.unit || "—" }}</td>
            <td>{{ p.readback_tolerance }}</td>
            <td :class="p.write_enabled ? 'quality-good' : ''">
              {{ p.write_enabled ? "已启用" : "已锁定" }}
            </td>
            <td>
              <button
                class="btn"
                :disabled="!p.write_enabled"
                @click="execute(p)"
              >
                执行写入
              </button>
              <button class="btn" @click="open(p)">编辑</button
              ><button class="btn" @click="remove(p.id)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>时间</th>
            <th>点位</th>
            <th>目标值</th>
            <th>回读值</th>
            <th>结果</th>
            <th>原因</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in logs" :key="l.id">
            <td>{{ l.created_at }}</td>
            <td>{{ l.point_name }}</td>
            <td>{{ l.target_value }}</td>
            <td>{{ l.readback_value ?? "—" }}</td>
            <td
              :class="l.result === 'success' ? 'quality-good' : 'quality-bad'"
            >
              {{ l.result }}
            </td>
            <td>{{ l.reason || "—" }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="!(tab === 'points' ? items : logs).length" class="empty">
        暂无数据
      </div>
    </div>
    <div v-if="show" class="modal-mask">
      <form class="modal" @submit.prevent="save">
        <h3>{{ editing ? "编辑写入点" : "新增写入点" }}</h3>
        <div class="form-grid">
          <label class="field"
            >名称<input v-model="form.name" class="input" required /></label
          ><label class="field"
            >分组<input v-model="form.group_name" class="input" required
          /></label>
          <label class="field"
            >设备<select v-model="form.device_id" class="select" required>
              <option v-for="d in devices" :key="d.id" :value="d.id">
                {{ d.name }}
              </option>
            </select></label
          ><label class="field"
            >数据类型<select v-model="form.data_type" class="select">
              <option>BOOL</option>
              <option>INT</option>
              <option>REAL</option>
            </select></label
          >
          <label class="field"
            >地址<input
              v-model="form.address"
              class="input"
              required
              placeholder="MD540" /></label
          ><label class="field"
            >单位<input v-model="form.unit" class="input" /></label
          ><label class="field"
            >回读容差<input
              v-model.number="form.readback_tolerance"
              class="input"
              type="number"
              min="0"
              step="any"
          /></label>
          <label class="field"
            ><span
              ><input v-model="form.enabled" type="checkbox" /> 启用</span
            ></label
          ><label class="field"
            ><span
              ><input v-model="form.write_enabled" type="checkbox" />
              允许写入</span
            ></label
          >
        </div>
        <div class="modal-actions">
          <button type="button" class="btn" @click="show = false">取消</button
          ><button class="btn btn-primary">保存</button>
        </div>
      </form>
    </div>
  </section>
</template>
