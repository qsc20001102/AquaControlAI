<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import {
  Download,
  FolderPlus,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  ShieldAlert,
  Trash2,
  Upload,
} from "lucide-vue-next";
import {
  collectionApi,
  deviceApi,
  exportConfig,
  importConfig,
  writePointApi,
} from "@/api/platform";

const items = ref<any[]>([]),
  logs = ref<any[]>([]),
  tab = ref<"points" | "logs">("points"),
  devices = ref<any[]>([]),
  groups = ref<any[]>([]),
  selectedGroup = ref(""),
  keyword = ref(""),
  loading = ref(false),
  show = ref(false),
  editing = ref<string | null>(null);

const form = reactive({
  name: "",
  group_name: "default",
  device_id: "",
  write_enabled: false,
  address: "",
  data_type: "REAL",
  unit: "",
});

async function load() {
  try {
    const [a, b]: any = await Promise.all([
      writePointApi.list({
        keyword: keyword.value,
        page_size: 100,
        group_name: selectedGroup.value,
      }),
      writePointApi.logs({ page_size: 100 }),
    ]);
    items.value = a.data.items;
    logs.value = b.data.items;
  } catch {
    items.value = [];
    logs.value = [];
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
  Object.assign(form, {
    name: p?.name ?? "",
    group_name: p?.group_name ?? groups.value[0]?.name ?? "default",
    device_id: p?.device_id ?? devices.value[0]?.id ?? "",
    write_enabled: p?.write_enabled ?? false,
    address: p?.address ?? "",
    data_type: p?.data_type ?? "REAL",
    unit: p?.unit ?? "",
  });
  show.value = true;
}

async function save() {
  try {
    editing.value
      ? await writePointApi.update(editing.value, form)
      : await writePointApi.create(form);
    show.value = false;
    await refresh();
  } catch (e) {
    alert(e instanceof Error ? e.message : "保存写入点失败");
  }
}

async function execute(p: any) {
  const raw = prompt(`向 ${p.name} (${p.address}) 写入值：`);
  if (raw === null) return;
  const value = p.data_type === "BOOL" ? raw === "true" : Number(raw);
  const reason = prompt("写入原因（可选）：") ?? "";
  const r: any = await writePointApi.write(p.id, { value, reason });
  const readback = r.data?.readback_value;
  alert(`写入成功，回读值：${readback ?? "—"}`);
  await refresh();
}

async function remove(id: string) {
  if (confirm("确认删除该写入点？")) {
    try {
      await writePointApi.remove(id);
      await refresh();
    } catch (e) {
      alert(e instanceof Error ? e.message : "删除写入点失败");
    }
  }
}

async function upload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0];
  if (!file) return;
  const r: any = await importConfig("write-points", file);
  alert(
    `导入完成：新增${r.data.created}，更新${r.data.updated}，失败${r.data.failed}`,
  );
  await refresh();
}

onMounted(async () => {
  const deviceResult: any = await deviceApi.list({ page_size: 100 });
  devices.value = deviceResult.data.items;
  await refresh();
});
</script>

<template>
  <section>
    <div class="collection-layout">
      <aside class="group-sidebar panel">
        <div class="group-header">
          <span>写入点分组</span>
          <button class="icon-btn" title="添加分组" @click="addGroup">
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
          <span>{{ groups.reduce((sum, g) => sum + g.write_count, 0) }}</span>
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
            <span>{{ g.name }}</span>
            <span>{{ g.write_count }}</span>
          </button>
          <span class="group-actions">
            <button class="icon-btn" title="修改分组" @click="editGroup(g)">
              <Pencil />
            </button>
            <button
              class="icon-btn danger"
              title="删除分组"
              @click="deleteGroup(g)"
            >
              <Trash2 />
            </button>
          </span>
        </div>
      </aside>

      <div class="collection-main">
        <div class="page-head">
          <div>
            <h2>数据写入</h2>
            <p>人工写入、回读数据与完整操作审计</p>
          </div>
          <div class="toolbar">
            <label class="btn">
              <Upload />导入CSV
              <input hidden type="file" accept=".csv" @change="upload" />
            </label>
            <button class="btn" @click="exportConfig('write-points')">
              <Download />导出CSV
            </button>
            <button class="btn" :disabled="loading" @click="refresh">
              <RefreshCw :class="loading && 'spin'" />刷新数据
            </button>
            <button class="btn btn-primary" @click="open()">
              <Plus />新增写入点
            </button>
          </div>
        </div>

        <div class="toolbar" style="margin-bottom: 12px">
          <input v-model="keyword" class="input" placeholder="搜索名称或分组" />
          <button class="btn" @click="load"><Search />查询</button>
        </div>

        <div class="toolbar" style="margin-bottom: 12px">
          <button
            v-if="tab === 'logs'"
            class="btn"
            @click="tab = 'points'"
          >
            写入点
          </button>
          <button
            v-if="tab === 'points'"
            class="btn"
            @click="tab = 'logs'"
          >
            操作日志
          </button>
          <span style="margin-left: auto; color: var(--warn); font-size: 12px">
            <ShieldAlert style="width: 15px; vertical-align: middle" />
            写入必须允许写入并完成回读验证
          </span>
        </div>

        <div class="panel">
          <table v-if="tab === 'points'" class="data-table">
            <thead>
              <tr>
                <th>名称</th>
                <th>分组</th>
                <th>所属设备</th>
                <th>地址</th>
                <th>类型</th>
                <th>单位</th>
                <th>回读数据</th>
                <th>允许写入</th>
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
                <td>{{ p.unit || "—" }}</td>
                <td>{{ p.readback_value ?? "—" }}</td>
                <td :class="p.write_enabled ? 'quality-good' : 'quality-bad'">
                  {{ p.write_enabled ? "允许" : "禁止" }}
                </td>
                <td>
                  <button
                    class="btn"
                    :disabled="!p.write_enabled"
                    @click="execute(p)"
                  >
                    执行写入
                  </button>
                  <button class="btn" @click="open(p)">编辑</button>
                  <button class="btn" @click="remove(p.id)">删除</button>
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
                  :class="
                    l.result === 'success' ? 'quality-good' : 'quality-bad'
                  "
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
      </div>
    </div>

    <div v-if="show" class="modal-mask">
      <form class="modal" @submit.prevent="save">
        <h3>{{ editing ? "编辑写入点" : "新增写入点" }}</h3>
        <div class="form-grid">
          <label class="field">
            名称<input v-model="form.name" class="input" required />
          </label>
          <label class="field">
            分组<select v-model="form.group_name" class="select" required>
              <option v-for="g in groups" :key="g.name" :value="g.name">
                {{ g.name }}
              </option>
            </select>
          </label>
          <label class="field">
            设备<select v-model="form.device_id" class="select" required>
              <option v-for="d in devices" :key="d.id" :value="d.id">
                {{ d.name }}
              </option>
            </select>
          </label>
          <label class="field">
            数据类型<select v-model="form.data_type" class="select">
              <option>BOOL</option>
              <option>INT</option>
              <option>REAL</option>
            </select>
          </label>
          <label class="field">
            地址<input
              v-model="form.address"
              class="input"
              required
            />
          </label>
          <label class="field">
            单位<input v-model="form.unit" class="input" />
          </label>
          <label class="check-field check-field-wide">
            <input v-model="form.write_enabled" type="checkbox" />
            <span class="check-box" aria-hidden="true"></span>
            <span class="check-text">允许写入</span>
          </label>
        </div>
        <div class="modal-actions">
          <button type="button" class="btn" @click="show = false">取消</button>
          <button class="btn btn-primary">保存</button>
        </div>
      </form>
    </div>
  </section>
</template>
