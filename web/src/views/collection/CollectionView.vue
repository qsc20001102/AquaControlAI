<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import {
  Download,
  Plus,
  RefreshCw,
  Search,
  Upload,
  FolderPlus,
  Pencil,
  Trash2,
} from "lucide-vue-next";
import {
  collectionApi,
  deviceApi,
  exportConfig,
  importConfig,
} from "@/api/platform";
const items = ref<any[]>([]),
  devices = ref<any[]>([]),
  loading = ref(false),
  keyword = ref(""),
  show = ref(false),
  editing = ref<string | null>(null);
const groups = ref<any[]>([]),
  selectedGroup = ref(""),
  groupEditor = ref<string | null>(null),
  groupName = ref("");
const form = reactive({
  name: "",
  group_name: "default",
  device_id: "",
  enabled: true,
  address: "",
  data_type: "REAL",
  unit: "",
  collect_interval: 1,
  store_history: true,
  history_interval: 1,
});
async function load() {
  try {
    const r: any = await collectionApi.list({
      keyword: keyword.value,
      page_size: 100,
      group_name: selectedGroup.value,
    });
    items.value = r.data.items;
  } catch {
    items.value = [];
  }
}
async function loadGroups() {
  const r: any = await collectionApi.groups();
  groups.value = r.data.groups;
}
async function refresh() {
  if (loading.value) return;
  loading.value = true;
  try {
    await Promise.all([loadGroups(), load()]);
  } catch {
    alert("刷新数据失败，请稍后重试");
  } finally {
    loading.value = false;
  }
}
async function addGroup() {
  const name = prompt("请输入新分组名称")?.trim();
  if (!name) return;
  await collectionApi.createGroup(name);
  selectedGroup.value = name;
  await refresh();
}
async function editGroup(g: any) {
  const name = prompt("修改分组名称", g.name)?.trim();
  if (!name || name === g.name) return;
  await collectionApi.updateGroup(g.name, name);
  if (selectedGroup.value === g.name) selectedGroup.value = name;
  await refresh();
}
async function deleteGroup(g: any) {
  if (g.name === "default") {
    alert("default 分组不可删除");
    return;
  }
  if (
    !confirm(
      `删除分组“${g.name}”后，里面的采集点和写入点会自动转移到 default 分组，是否继续？`,
    )
  )
    return;
  await collectionApi.removeGroup(g.name);
  if (selectedGroup.value === g.name) selectedGroup.value = "";
  await refresh();
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
      address: "",
      data_type: "REAL",
      unit: "",
      collect_interval: 1,
      store_history: true,
      history_interval: 1,
    },
  );
  if (!form.group_name && groups.value.length)
    form.group_name = groups.value[0].name;
  show.value = true;
}
async function save() {
  try {
    editing.value
      ? await collectionApi.update(editing.value, form)
      : await collectionApi.create(form);
    show.value = false;
    await refresh();
  } catch (e) {
    alert(e instanceof Error ? e.message : "保存采集点失败");
  }
}
async function remove(id: string) {
  if (confirm("确认删除该采集点？")) {
    try {
      await collectionApi.remove(id);
      await refresh();
    } catch (e) {
      alert(e instanceof Error ? e.message : "删除采集点失败");
    }
  }
}
async function upload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0];
  if (!file) return;
  const r: any = await importConfig("collection-points", file);
  alert(
    `导入完成：新增${r.data.created}，更新${r.data.updated}，失败${r.data.failed}`,
  );
  await refresh();
}
onMounted(async () => {
  const r: any = await deviceApi.list({ page_size: 100 });
  devices.value = r.data.items;
  await refresh();
});
function formatTime(value?: string) {
  return value
    ? new Date(value).toLocaleString("zh-CN", { hour12: false })
    : "—";
}
</script>
<template>
  <section>
    <div class="collection-layout">
      <aside class="group-sidebar panel">
        <div class="group-header">
          <span>采集点分组</span
          ><button class="icon-btn" title="添加分组" @click="addGroup">
            <FolderPlus />
          </button>
        </div>
        <button
          :class="['group-item', !selectedGroup && 'active']"
          @click="
            selectedGroup = '';
            load();
          "
        >
          全部分组
          <span>{{ groups.reduce((sum, g) => sum + g.count, 0) }}</span>
        </button>
        <div
          v-for="g in groups"
          :key="g.name"
          :class="['group-item-wrap', selectedGroup === g.name && 'active']"
        >
          <button
            :class="['group-item', selectedGroup === g.name && 'active']"
            @click="
              selectedGroup = g.name;
              load();
            "
          >
            <span>{{ g.name }}</span
            ><span>{{ g.count }}</span>
          </button>
          <span class="group-actions"
            ><button class="icon-btn" title="修改分组" @click="editGroup(g)">
              <Pencil /></button
            ><button
              class="icon-btn danger"
              title="删除分组"
              @click="deleteGroup(g)"
            >
              <Trash2 /></button
          ></span>
        </div>
      </aside>
      <div class="collection-main">
        <div class="page-head">
          <div>
            <h2>数据采集</h2>
            <p>点位地址、采集周期、历史策略与实时质量</p>
          </div>
          <div class="toolbar">
            <label class="btn"
              ><Upload />导入CSV<input
                hidden
                type="file"
                accept=".csv"
                @change="upload"
            /></label>
            <button class="btn" @click="exportConfig('collection-points')">
              <Download />导出CSV
            </button>
            <button class="btn" :disabled="loading" @click="refresh">
              <RefreshCw :class="loading && 'spin'" />刷新数据
            </button>
            <button class="btn btn-primary" @click="open()">
              <Plus />新增采集点
            </button>
          </div>
        </div>
        <div class="toolbar" style="margin-bottom: 12px">
          <input
            v-model="keyword"
            class="input"
            placeholder="搜索名称或分组"
          /><button class="btn" @click="load"><Search />查询</button>
        </div>
        <div class="panel">
          <table class="data-table">
            <thead>
              <tr>
                <th>名称</th>
                <th>分组</th>
                <th>所属设备</th>
                <th>地址</th>
                <th>类型</th>
                <th>采集周期</th>
                <th>历史</th>
                <th>最新值</th>
                <th>质量</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in items" :key="p.id">
                <td>{{ p.name }}</td>
                <td>{{ p.group_name }}</td>
                <td>{{ p.device_name }}</td>
                <td>{{ p.address }}</td>
                <td>
                  <span class="tag">{{ p.data_type }}</span>
                </td>
                <td>{{ p.collect_interval }} 秒</td>
                <td>
                  {{ p.store_history ? p.history_interval + " 分钟" : "关闭" }}
                </td>
                <td>{{ p.latest_value?.value ?? "—" }} {{ p.unit }}</td>
                <td
                  :class="
                    p.latest_value?.quality === 'bad'
                      ? 'quality-bad'
                      : 'quality-good'
                  "
                >
                  {{ p.latest_value?.quality ?? "none" }}
                </td>
                <td>{{ formatTime(p.latest_value?.ts) }}</td>
                <td>
                  <button class="btn" @click="open(p)">编辑</button>
                  <button class="btn" @click="remove(p.id)">删除</button>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-if="!items.length" class="empty">暂无采集点</div>
        </div>
      </div>
    </div>
    <div v-if="show" class="modal-mask">
      <form class="modal" @submit.prevent="save">
        <h3>{{ editing ? "编辑采集点" : "新增采集点" }}</h3>
        <div class="form-grid">
          <label class="field"
            >名称<input
              v-model="form.name"
              class="input"
              required
              maxlength="128" /></label
          ><label class="field"
            >分组<select v-model="form.group_name" class="select" required>
              <option v-for="g in groups" :key="g.name" :value="g.name">
                {{ g.name }}
              </option>
            </select></label
          >
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
              placeholder="DB2.1186.0" /></label
          ><label class="field"
            >单位<input v-model="form.unit" class="input" maxlength="32"
          /></label>
          <label class="field"
            >采集周期（秒）<input
              v-model.number="form.collect_interval"
              class="input"
              type="number"
              min="1" /></label
          ><label class="field"
            >历史间隔（分钟）<input
              v-model.number="form.history_interval"
              class="input"
              type="number"
              min="1"
              max="1440"
          /></label>
          <label class="check-field">
            <input v-model="form.enabled" type="checkbox" />
            <span class="check-box" aria-hidden="true"></span>
            <span class="check-text">启用采集</span>
          </label>
          <label class="check-field">
            <input v-model="form.store_history" type="checkbox" />
            <span class="check-box" aria-hidden="true"></span>
            <span class="check-text">存储历史</span>
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
