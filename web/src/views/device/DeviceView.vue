<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { Download, Plus, RefreshCw, Search, Upload } from "lucide-vue-next";
import { deviceApi, exportConfig, importConfig } from "@/api/platform";
type Device = {
  id: string;
  name: string;
  protocol_type: string;
  host: string;
  port: number;
  enabled: boolean;
  connection_status: string;
  last_online_at?: string;
  last_offline_at?: string;
  protocol_config: Record<string, number | string>;
};
const items = ref<Device[]>([]),
  loading = ref(false),
  show = ref(false),
  editing = ref<string | null>(null),
  keyword = ref("");
const form = reactive({
  name: "",
  protocol_type: "S7",
  host: "192.168.107.10",
  port: 102,
  connect_timeout: 5,
  reconnect_interval: 5,
  enabled: true,
  rack: 0,
  slot: 0,
  unit_id: 1,
  float32_order: "CDAB",
});
function applyProtocolDefaults() {
  if (form.protocol_type === "S7") {
    form.port = 102;
    form.rack = 0;
    form.slot = 0;
    return;
  }
  form.port = 502;
  form.unit_id = 1;
  form.float32_order = "CDAB";
}
async function load() {
  loading.value = true;
  try {
    const r: any = await deviceApi.list({
      keyword: keyword.value,
      page_size: 100,
    });
    items.value = r.data.items;
  } catch {
    items.value = [];
  } finally {
    loading.value = false;
  }
}
function open(d?: Device) {
  editing.value = d?.id ?? null;
  Object.assign(
    form,
    d
        ? {
            ...d,
            rack: d.protocol_config.rack ?? 0,
            slot: d.protocol_config.slot ?? 0,
            unit_id: d.protocol_config.unit_id ?? 1,
            float32_order: String(d.protocol_config.float32_order ?? "CDAB"),
          }
        : {
            name: "",
            protocol_type: "S7",
            host: "192.168.107.10",
            port: 102,
            connect_timeout: 5,
            reconnect_interval: 5,
            enabled: true,
            rack: 0,
            slot: 0,
            unit_id: 1,
            float32_order: "CDAB",
          },
  );
  show.value = true;
}
async function save() {
  const data = {
    ...form,
    protocol_config:
      form.protocol_type === "S7"
        ? { rack: form.rack, slot: form.slot }
        : { unit_id: form.unit_id, float32_order: form.float32_order },
  };
  try {
    editing.value
      ? await deviceApi.update(editing.value, data)
      : await deviceApi.create(data);
    show.value = false;
    await load();
  } catch (e) {
    alert(e instanceof Error ? e.message : "保存设备失败");
  }
}
async function remove(id: string) {
  if (confirm("确认删除？该设备下所有采集点和写入点将同步逻辑删除。")) {
    try {
      await deviceApi.remove(id);
      await load();
    } catch (e) {
      alert(e instanceof Error ? e.message : "删除设备失败");
    }
  }
}
onMounted(load);
async function upload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0];
  if (!file) return;
  const r: any = await importConfig("devices", file);
  alert(
    `导入完成：新增${r.data.created}，更新${r.data.updated}，失败${r.data.failed}`,
  );
  await load();
}
function statusLabel(status: string) {
  return status === "connected"
    ? "在线"
    : status === "disabled"
      ? "禁用"
      : "离线";
}
function statusClass(status: string) {
  return status === "connected"
    ? "online"
    : status === "disabled"
      ? "disabled"
      : "offline";
}
function formatTime(value?: string) {
  return value
    ? new Date(value).toLocaleString("zh-CN", { hour12: false })
    : "—";
}
</script>
<template>
  <section>
    <div class="page-head">
      <div>
        <h2>设备管理</h2>
        <p>管理 PLC 连接、协议参数与运行时连接状态</p>
      </div>
      <div class="toolbar">
        <label class="btn"
          ><Upload />导入CSV<input
            hidden
            type="file"
            accept=".csv"
            @change="upload"
        /></label>
        <button class="btn" @click="exportConfig('devices')">
          <Download />导出CSV
        </button>
        <button class="btn" :disabled="loading" @click="load">
          <RefreshCw :class="loading && 'spin'" />刷新数据
        </button>
        <button class="btn btn-primary" @click="open()">
          <Plus />新增设备
        </button>
      </div>
    </div>
    <div class="toolbar" style="margin-bottom: 12px">
      <input
        v-model="keyword"
        class="input"
        placeholder="搜索设备名称"
        @keyup.enter="load"
      /><button class="btn" @click="load"><Search />查询</button>
    </div>
    <div class="panel">
      <table class="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>协议</th>
            <th>地址</th>
            <th>端口</th>
            <th>设备状态</th>
            <th>最近在线</th>
            <th>最近离线</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in items" :key="d.id">
            <td>{{ d.name }}</td>
            <td>
              <span class="tag">{{ d.protocol_type }}</span>
            </td>
            <td>{{ d.host }}</td>
            <td>{{ d.port }}</td>
            <td>
              <span
                :class="['status-pill', statusClass(d.connection_status)]"
                >{{ statusLabel(d.connection_status) }}</span
              >
            </td>
            <td>{{ formatTime(d.last_online_at) }}</td>
            <td>{{ formatTime(d.last_offline_at) }}</td>
            <td>
              <button class="btn" @click="open(d)">编辑</button>
              <button class="btn" @click="remove(d.id)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="!loading && !items.length" class="empty">
        暂无设备，点击“新增设备”开始配置
      </div>
    </div>
    <div v-if="show" class="modal-mask">
      <form class="modal" @submit.prevent="save">
        <h3>{{ editing ? "编辑设备" : "新增设备" }}</h3>
        <div class="form-grid">
          <label class="field"
            >名称<input
              v-model="form.name"
              class="input"
              required
              maxlength="128" /></label
          ><label class="field"
            >协议<select
              v-model="form.protocol_type"
              class="select"
              @change="applyProtocolDefaults"
            >
              <option>S7</option>
              <option>MODBUS_TCP</option>
            </select></label
          ><label class="field"
            >主机<input v-model="form.host" class="input" required /></label
          ><label class="field"
            >端口<input
              v-model.number="form.port"
              class="input"
              type="number"
              min="1"
              max="65535" /></label
          ><template v-if="form.protocol_type === 'S7'"
            ><label class="field"
              >Rack<input
                v-model.number="form.rack"
                class="input"
                type="number" /></label
            ><label class="field"
              >Slot<input
                v-model.number="form.slot"
                class="input"
                type="number" /></label></template
          ><template v-else
            ><label class="field"
              >站号<input
                v-model.number="form.unit_id"
                class="input"
                type="number"
                min="1"
                max="247"
                required /></label
            ><label class="field"
              >REAL 字节序<select v-model="form.float32_order" class="select">
                <option>ABCD</option>
                <option>BADC</option>
                <option>CDAB</option>
                <option>DCBA</option>
              </select></label
            ></template
          ><label class="field"
            >连接超时（秒）<input
              v-model.number="form.connect_timeout"
              class="input"
              type="number"
              min="1"
              max="60" /></label
          ><label class="field"
            >重连间隔（秒）<input
              v-model.number="form.reconnect_interval"
              class="input"
              type="number"
              min="1"
              max="3600" /></label
          ><label class="check-field check-field-wide">
            <input v-model="form.enabled" type="checkbox" />
            <span class="check-box" aria-hidden="true"></span>
            <span class="check-text">启用设备</span>
          </label>
        </div>
        <div class="modal-actions">
          <button type="button" class="btn" @click="show = false">取消</button
          ><button class="btn btn-primary">保存</button>
        </div>
      </form>
    </div>
  </section>
</template>
