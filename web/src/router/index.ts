import { createRouter, createWebHistory } from "vue-router";
import DeviceView from "@/views/device/DeviceView.vue";
import CollectionView from "@/views/collection/CollectionView.vue";
import WritePointView from "@/views/write-point/WritePointView.vue";
import HistoryView from "@/views/history/HistoryView.vue";

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/history" },
    {
      path: "/data/device",
      component: DeviceView,
      meta: { title: "设备管理" },
    },
    {
      path: "/data/collection",
      component: CollectionView,
      meta: { title: "数据采集" },
    },
    {
      path: "/data/write-point",
      component: WritePointView,
      meta: { title: "数据写入" },
    },
    { path: "/history", component: HistoryView, meta: { title: "历史数据" } },
  ],
});
