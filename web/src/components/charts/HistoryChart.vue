<script setup lang="ts">
import * as echarts from "echarts";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { buildSegmentedAxis, mapValue, unmapValue } from "./segmented-axis";

const props = defineProps<{
  series: any[];
  segmented: boolean;
  startTime: number;
  endTime: number;
}>();
const emit = defineEmits<{ cursor: [any[]] }>();
const el = ref<HTMLDivElement>();
let chart: echarts.ECharts | undefined;
let observer: ResizeObserver | undefined;
let currentNames: string[] = [];
let visibleNames: Record<string, boolean> = {};
let lastCursorTime: number | null = null;
let cursorFrame: number | undefined;
let draggingCursor = false;
const gridTop = 58;
const gridBottom = 52;

const segments = computed(() =>
  buildSegmentedAxis(
    props.series.flatMap((s) =>
      (s.data ?? [])
        .filter((d: any) => d.value !== null && d.value !== undefined)
        .map((d: any) => d.value),
    ),
  ),
);
const range = computed(() => Math.max(1, props.endTime - props.startTime || 1));
const colors = [
  "#63f04f",
  "#18d7e9",
  "#f3bd42",
  "#c68cff",
  "#ff8f66",
  "#73b7ff",
];

function timestamp(value: any) {
  if (typeof value === "number") return value;
  const result = new Date(value).getTime();
  return Number.isFinite(result) ? result : NaN;
}
function displayValue(value: any) {
  return typeof value === "number" ? Number(value.toFixed(3)) : value;
}
function displayDateTime(value: any) {
  const time = timestamp(value);
  if (!Number.isFinite(time)) return "—";
  return new Date(time).toLocaleString("zh-CN", {
    hour12: false,
    timeZone: "Asia/Shanghai",
  });
}
function axisLabel(value: number) {
  const date = new Date(value);
  const parts = new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).formatToParts(date);
  const part = (type: string) =>
    parts.find((x) => x.type === type)?.value ?? "";
  if (range.value <= 2 * 3600000)
    return `${part("hour")}:${part("minute")}:${part("second")}`;
  return `${part("month")}-${part("day")} ${part("hour")}:${part("minute")}`;
}
function splitNumber() {
  if (range.value <= 2 * 3600000) return 8;
  if (range.value <= 24 * 3600000) return 10;
  if (range.value <= 7 * 24 * 3600000) return 8;
  return 7;
}
function seriesName(s: any, index: number, used: Set<string>) {
  const base = `${s.point_name}${s.unit ? ` (${s.unit})` : ""}`;
  if (!used.has(base)) {
    used.add(base);
    return base;
  }
  const name = `${base} #${index + 1}`;
  used.add(name);
  return name;
}
function pointRows(s: any) {
  return (s.data ?? [])
    .map((d: any) => ({ ...d, time: timestamp(d.ts) }))
    .filter((d: any) => Number.isFinite(d.time))
    .sort((a: any, b: any) => a.time - b.time);
}
function cursorRow(s: any, target: number) {
  const points = pointRows(s);
  const cursorTS = new Date(target).toISOString();
  const base = {
    pointId: s.point_id,
    pointName: s.point_name,
    ts: cursorTS,
    value: null as number | null,
    quality: "none",
    qualityReason: null as string | null,
    unit: s.unit,
    interpolated: false,
  };
  if (
    !points.length ||
    target < points[0].time ||
    target > points.at(-1).time
  ) {
    return base;
  }
  let nearest = points[0];
  for (const point of points) {
    if (Math.abs(point.time - target) < Math.abs(nearest.time - target)) {
      nearest = point;
    }
  }
  if (Math.abs(nearest.time - target) <= 1) {
    return {
      ...base,
      value: nearest.value ?? null,
      quality: nearest.quality ?? "none",
      qualityReason: nearest.quality_reason ?? null,
    };
  }
  const numeric = points.filter(
    (point: any) => point.value !== null && point.value !== undefined,
  );
  if (!numeric.length) {
    return {
      ...base,
      quality: nearest.quality ?? "none",
      qualityReason: nearest.quality_reason ?? null,
    };
  }
  let before: any;
  let after: any;
  for (const point of numeric) {
    if (point.time <= target) before = point;
    if (point.time >= target) {
      after = point;
      break;
    }
  }
  if (before && after && before.time !== after.time) {
    const blockedBetween = points.some(
      (point: any) =>
        point.time > before.time &&
        point.time < after.time &&
        (point.value === null ||
          point.value === undefined ||
          point.quality !== "good"),
    );
    if (blockedBetween) {
      return base;
    }
    const ratio = (target - before.time) / (after.time - before.time);
    const badBetween = points.some(
      (point: any) =>
        point.time > before.time &&
        point.time < after.time &&
        point.quality === "bad",
    );
    return {
      ...base,
      value: before.value + (after.value - before.value) * ratio,
      quality:
        before.quality === "bad" || after.quality === "bad" || badBetween
          ? "bad"
          : "good",
      qualityReason: before.quality_reason ?? after.quality_reason ?? null,
      interpolated: true,
    };
  }
  const edge = before ?? after;
  return {
    ...base,
    value: edge.value ?? null,
    quality: edge.quality ?? "none",
    qualityReason: edge.quality_reason ?? null,
    interpolated: true,
  };
}
function cursorRows(target: number) {
  return props.series
    .map((s: any, index: number) => {
      const name = currentNames[index];
      return visibleNames[name] === false ? null : cursorRow(s, target);
    })
    .filter(Boolean);
}
function clampCursorTime(target: number) {
  return Math.min(Math.max(target, props.startTime), props.endTime);
}
function midpointCursorTime() {
  return props.startTime + (props.endTime - props.startTime) / 2;
}
function emitCursorAt(target: number) {
  if (!Number.isFinite(target)) return;
  lastCursorTime = clampCursorTime(target);
  scheduleVisualCursor(lastCursorTime);
  emit("cursor", cursorRows(lastCursorTime));
}
function scheduleVisualCursor(target: number) {
  if (cursorFrame !== undefined) cancelAnimationFrame(cursorFrame);
  cursorFrame = requestAnimationFrame(() => {
    cursorFrame = undefined;
    updateVisualCursor(target);
  });
}
function updateVisualCursor(target: number) {
  if (!chart) return;
  const pixel = chart.convertToPixel({ gridIndex: 0 }, [target, 0]) as number[];
  if (!Number.isFinite(pixel?.[0])) return;
  chart.setOption(
    {
      graphic: [
        {
          id: "history-cursor-line",
          type: "line",
          left: pixel[0],
          top: gridTop,
          shape: {
            x1: 0,
            y1: 0,
            x2: 0,
            y2: Math.max(1, chart.getHeight() - gridTop - gridBottom),
          },
          style: {
            stroke: "#dbeee8",
            lineWidth: 1,
            lineDash: [5, 5],
            opacity: 0.9,
          },
          animation: false,
          silent: true,
          z: 100,
        },
      ],
    },
    { lazyUpdate: true },
  );
}
function pointerTargetTime(event: any) {
  if (!chart) return;
  const x = event.zrX ?? event.offsetX;
  const y = event.zrY ?? event.offsetY;
  if (!Number.isFinite(x) || !Number.isFinite(y)) return;
  const converted = chart.convertFromPixel({ gridIndex: 0 }, [
    x,
    y,
  ]) as number[];
  const target = timestamp(converted?.[0]);
  return Number.isFinite(target) ? target : undefined;
}
function updateCursorFromPointer(event: any) {
  const target = pointerTargetTime(event);
  if (target !== undefined) emitCursorAt(target);
}
function isPrimaryMouseDown(event: any) {
  const native = event.event;
  return native?.button === undefined || native.button === 0;
}
function isPrimaryButtonStillPressed(event: any) {
  const native = event.event;
  return native?.buttons === undefined || (native.buttons & 1) === 1;
}
function handleMouseDown(event: any) {
  if (!isPrimaryMouseDown(event)) return;
  draggingCursor = true;
  updateCursorFromPointer(event);
}
function handleMouseMove(event: any) {
  if (!draggingCursor) return;
  if (!isPrimaryButtonStillPressed(event)) {
    draggingCursor = false;
    return;
  }
  updateCursorFromPointer(event);
}
function stopCursorDrag() {
  draggingCursor = false;
}
function handleLegendChange(event: any) {
  visibleNames = { ...(event.selected ?? {}) };
  if (lastCursorTime !== null) emitCursorAt(lastCursorTime);
}
function render() {
  if (!chart) return;
  const option = (chart.getOption?.() as any) ?? {};
  const previous = option.legend?.[0]?.selected ?? {};
  const used = new Set<string>();
  currentNames = props.series.map((s: any, i: number) =>
    seriesName(s, i, used),
  );
  const selected: Record<string, boolean> = {};
  currentNames.forEach((name) => {
    selected[name] = previous[name] !== false;
  });
  visibleNames = selected;
  const chartSeries: any[] = [];
  props.series.forEach((s: any, i: number) => {
    const solid: any[] = [];
    const bad: any[] = [];
    (s.data ?? []).forEach((d: any) => {
      const numeric = d.value !== null && d.value !== undefined;
      const mapped = numeric
        ? props.segmented
          ? mapValue(d.value, segments.value)
          : d.value
        : null;
      const row = [
        d.ts,
        mapped,
        d.ts,
        d.value,
        d.quality,
        s.unit,
        s.point_name,
        s.point_id,
        d.quality_reason,
      ];
      if (d.quality === "bad") {
        solid.push([
          d.ts,
          null,
          d.ts,
          null,
          d.quality,
          s.unit,
          s.point_name,
          s.point_id,
          d.quality_reason,
        ]);
        bad.push([
          d.ts,
          numeric ? mapped : null,
          d.ts,
          d.value,
          d.quality,
          s.unit,
          s.point_name,
          s.point_id,
          d.quality_reason,
        ]);
      } else {
        solid.push(row);
        bad.push([
          d.ts,
          null,
          d.ts,
          null,
          d.quality,
          s.unit,
          s.point_name,
          s.point_id,
          d.quality_reason,
        ]);
      }
    });
    const color = colors[i % colors.length];
    chartSeries.push(
      {
        id: `${s.point_id}-good`,
        name: currentNames[i],
        type: "line",
        showSymbol: true,
        symbolSize: 4,
        connectNulls: false,
        data: solid,
        lineStyle: { width: 1.8, color },
        itemStyle: { color, opacity: 0.82 },
        emphasis: { focus: "series" },
      },
      {
        id: `${s.point_id}-bad`,
        name: currentNames[i],
        type: "line",
        showSymbol: true,
        symbolSize: 5,
        connectNulls: false,
        data: bad,
        lineStyle: { width: 1.8, type: "dashed", color },
        itemStyle: { color },
        emphasis: { focus: "series" },
      },
    );
  });
  chart.setOption(
    {
      animation: true,
      animationDuration: 260,
      animationDurationUpdate: 260,
      animationEasingUpdate: "cubicOut",
      color: colors,
      grid: {
        left: 62,
        right: 28,
        top: currentNames.length ? 58 : 30,
        bottom: 52,
      },
      graphic: [],
      legend: {
        show: currentNames.length > 0,
        data: currentNames,
        selected,
        top: 12,
        textStyle: { color: "#a9bbc0" },
        itemWidth: 18,
        itemHeight: 8,
      },
      tooltip: {
        trigger: "item",
        transitionDuration: 0.12,
        backgroundColor: "#07141af2",
        borderColor: "#35505a",
        textStyle: { color: "#dce9e5" },
        formatter: (param: any) => {
          const row = param?.data ?? [];
          if (row[1] === null || row[1] === undefined) return "";
          const unit = row[5] ? ` ${row[5]}` : "";
          const reason = row[8] ? `<br/>原因：${row[8]}` : "";
          return [
            `${row[6] ?? param.seriesName}`,
            `时间：${displayDateTime(row[2] ?? row[0])}`,
            `数值：${displayValue(row[3]) ?? "—"}${unit}`,
            `质量：${row[4] ?? "none"}${reason}`,
          ].join("<br/>");
        },
      },
      xAxis: {
        type: "time",
        min: props.startTime,
        max: props.endTime,
        splitNumber: splitNumber(),
        minInterval: range.value / 24,
        maxInterval: range.value / 3,
        axisLine: { lineStyle: { color: "#35505a" } },
        axisLabel: {
          color: "#718990",
          hideOverlap: true,
          formatter: axisLabel,
        },
        splitLine: { show: true, lineStyle: { color: "#142b33" } },
      },
      yAxis: {
        type: "value",
        min: props.segmented ? 0 : undefined,
        max: props.segmented ? 1 : undefined,
        scale: !props.segmented,
        axisLabel: {
          color: "#789098",
          formatter: (v: number) =>
            props.segmented
              ? unmapValue(v, segments.value).toFixed(2)
              : String(v),
        },
        splitLine: { lineStyle: { color: "#173039", type: "dashed" } },
      },
      series: chartSeries,
    },
    true,
  );
  const target =
    lastCursorTime === null
      ? midpointCursorTime()
      : clampCursorTime(lastCursorTime);
  emitCursorAt(target);
}
onMounted(() => {
  chart = echarts.init(el.value!);
  observer = new ResizeObserver(() => {
    chart?.resize();
    if (chart && lastCursorTime !== null) scheduleVisualCursor(lastCursorTime);
  });
  observer.observe(el.value!);
  chart.getZr().on("mousedown", handleMouseDown);
  chart.getZr().on("mousemove", handleMouseMove);
  chart.getZr().on("mouseup", stopCursorDrag);
  chart.getZr().on("globalout", stopCursorDrag);
  chart.on("legendselectchanged", handleLegendChange);
  document.addEventListener("mouseup", stopCursorDrag);
  window.addEventListener("blur", stopCursorDrag);
  render();
});
watch(
  () => [props.series, props.segmented, props.startTime, props.endTime],
  render,
  { deep: true },
);
onBeforeUnmount(() => {
  if (cursorFrame !== undefined) cancelAnimationFrame(cursorFrame);
  observer?.disconnect();
  chart?.getZr().off("mousedown", handleMouseDown);
  chart?.getZr().off("mousemove", handleMouseMove);
  chart?.getZr().off("mouseup", stopCursorDrag);
  chart?.getZr().off("globalout", stopCursorDrag);
  chart?.off("legendselectchanged", handleLegendChange);
  document.removeEventListener("mouseup", stopCursorDrag);
  window.removeEventListener("blur", stopCursorDrag);
  chart?.dispose();
});
</script>

<template><div ref="el" class="history-chart" /></template>

<style scoped>
.history-chart {
  width: 100%;
  height: 520px;
  background:
    linear-gradient(#0a1a21aa, #07151baa),
    repeating-linear-gradient(0deg, transparent 0 31px, #122a321f 32px);
}
</style>
