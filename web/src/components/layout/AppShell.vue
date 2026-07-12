<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import {
  Activity,
  Clock3,
  ChevronDown,
  Database,
  HardDrive,
  History,
  PenLine,
  RadioTower,
  Server,
  Waves,
} from "lucide-vue-next";
const route = useRoute();
const now = ref(new Date());
const timer = window.setInterval(() => (now.value = new Date()), 1000);
onBeforeUnmount(() => clearInterval(timer));
const title = computed(() => String(route.meta.title ?? "AquaControl AI"));
const dataExpanded = ref(route.path.startsWith("/data/"));
const dataActive = computed(() => route.path.startsWith("/data/"));
watch(() => route.path, (path) => { if (path.startsWith("/data/")) dataExpanded.value = true });
</script>
<template>
  <div class="app-shell">
    <header class="topbar">
      <div class="brand"><Waves :size="24" /><span>AquaControl AI</span></div>
      <h1>{{ title }}</h1>
      <div class="health">
        <span
          ><Clock3 />{{ now.toLocaleString("zh-CN", { hour12: false }) }}</span
        ><span><Database /><i />PostgreSQL</span
        ><span><HardDrive /><i />TDengine</span
        ><span><RadioTower /><i />PLC</span>
      </div>
    </header>
    <aside class="sidebar">
      <div class="nav-section">
        <button :class="['nav-heading', dataActive && 'module-active']" :aria-expanded="dataExpanded" @click="dataExpanded = !dataExpanded">
          <Database /><span>数据管理</span><ChevronDown class="nav-chevron" :class="dataExpanded && 'expanded'" />
        </button>
        <div v-show="dataExpanded" class="nav-children">
          <router-link to="/data/device"><Server />设备管理</router-link>
          <router-link to="/data/collection"><Activity />数据采集</router-link>
          <router-link to="/data/write-point"><PenLine />数据写入</router-link>
        </div>
      </div>
      <router-link class="top-level-link" to="/history"
        ><History />历史数据</router-link
      >
      <div class="system-state">
        <i /> 系统运行正常<small>配置变更 SLA ≤ 5 秒</small>
      </div>
    </aside>
    <main><slot /></main>
  </div>
</template>
