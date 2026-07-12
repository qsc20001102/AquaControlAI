<script setup lang="ts">
import * as echarts from "echarts";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { buildSegmentedAxis, mapValue, unmapValue } from "./segmented-axis";
const props = defineProps<{ series: any[]; segmented: boolean }>();
const emit = defineEmits<{ cursor: [any[]] }>();
const el = ref<HTMLDivElement>();
let chart: echarts.ECharts | undefined, observer: ResizeObserver | undefined;
const segments = computed(() =>
  buildSegmentedAxis(
    props.series.flatMap((s) =>
      s.data.filter((d: any) => d.value !== null).map((d: any) => d.value),
    ),
  ),
);
function render() {
  if (!chart) return;
  const colors = ["#63f04f", "#18d7e9", "#f3bd42", "#c68cff"];
  chart.setOption(
    {
      animationDuration: 350,
      color: colors,
      grid: { left: 58, right: 26, top: 58, bottom: 46 },
      legend: { top: 12, textStyle: { color: "#a9bbc0" } },
      tooltip: {
        trigger: "axis",
        axisPointer: {
          type: "line",
          lineStyle: { color: "#dbeee8", type: "dashed" },
        },
        backgroundColor: "#07141af2",
        borderColor: "#35505a",
        textStyle: { color: "#dce9e5" },
        formatter: (params: any) => {
          emit(
            "cursor",
            params.map((p: any) => ({
              pointName: p.seriesName,
              ts: p.data?.[2],
              value: p.data?.[3],
              quality: p.data?.[4],
              unit: p.data?.[5],
            })),
          );
          return params
            .map(
              (p: any) =>
                `${p.marker}${p.seriesName}<br/>${p.data?.[3] ?? "—"} ${p.data?.[5] ?? ""} · ${p.data?.[4]}`,
            )
            .join("<br/>");
        },
      },
      xAxis: {
        type: "time",
        axisLine: { lineStyle: { color: "#35505a" } },
        axisLabel: { color: "#718990" },
        splitLine: { show: true, lineStyle: { color: "#142b33" } },
      },
      yAxis: {
        type: "value",
        min: props.segmented ? 0 : undefined,
        max: props.segmented ? 1 : undefined,
        axisLabel: {
          color: "#789098",
          formatter: (v: number) =>
            props.segmented
              ? unmapValue(v, segments.value).toFixed(2)
              : String(v),
        },
        splitLine: { lineStyle: { color: "#173039", type: "dashed" } },
      },
      series: props.series.flatMap((s: any, i: number) => {
        const solid: any[] = [];
        const bad: any[] = [];
        s.data.forEach((d: any) => {
          const row = [
            d.ts,
            d.value === null
              ? null
              : props.segmented
                ? mapValue(d.value, segments.value)
                : d.value,
            d.ts,
            d.value,
            d.quality,
            s.unit,
          ];
          (d.quality === "bad" && d.value !== null ? bad : solid).push(row);
        });
        return [
          {
            name: `${s.point_name}${s.unit ? ` (${s.unit})` : ""}`,
            type: "line",
            showSymbol: false,
            connectNulls: false,
            data: solid,
            lineStyle: { width: 1.6, color: colors[i % colors.length] },
          },
          {
            name: `${s.point_name} · bad`,
            type: "line",
            showSymbol: true,
            symbolSize: 5,
            connectNulls: false,
            data: bad,
            lineStyle: { width: 1.4, type: "dashed", color: "#ff665c" },
            itemStyle: { color: "#ff665c" },
          },
        ];
      }),
    },
    true,
  );
}
onMounted(() => {
  chart = echarts.init(el.value!);
  observer = new ResizeObserver(() => chart?.resize());
  observer.observe(el.value!);
  render();
});
watch(() => [props.series, props.segmented], render, { deep: true });
onBeforeUnmount(() => {
  observer?.disconnect();
  chart?.dispose();
});
</script>
<template><div ref="el" class="history-chart" /></template>
<style scoped>
.history-chart {
  height: 450px;
  width: 100%;
  background:
    linear-gradient(#0a1a21aa, #07151baa),
    repeating-linear-gradient(0deg, transparent 0 31px, #122a321f 32px);
}
</style>
