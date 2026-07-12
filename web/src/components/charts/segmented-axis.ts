export interface AxisSegment {
  min: number;
  max: number;
  displayMin: number;
  displayMax: number;
}
export function buildSegmentedAxis(values: number[]): AxisSegment[] {
  if (values.length < 2) return [];
  const sorted = [...values].sort((a, b) => a - b);
  const clusters: { min: number; max: number }[] = [];
  const span = Math.max(sorted.at(-1)! - sorted[0], 1e-9);
  for (const value of sorted) {
    const last = clusters.at(-1);
    if (!last || value - last.max > span * 0.15)
      clusters.push({ min: value, max: value });
    else last.max = value;
  }
  const padded = clusters.map((c) => {
    const p = Math.max((c.max - c.min) * 0.15, span * 0.01);
    return { min: c.min - p, max: c.max + p };
  });
  let cursor = 0;
  return padded.map((c, i) => {
    const weight = 1 / padded.length;
    const result = {
      ...c,
      displayMin: cursor,
      displayMax: i === padded.length - 1 ? 1 : cursor + weight * 0.82,
    };
    cursor += weight;
    return result;
  });
}
export function mapValue(value: number, segments: AxisSegment[]): number {
  if (!segments.length) return value;
  const s =
    segments.find((x) => value >= x.min && value <= x.max) ??
    segments.reduce((a, b) =>
      Math.abs(value - (a.min + a.max) / 2) <
      Math.abs(value - (b.min + b.max) / 2)
        ? a
        : b,
    );
  return (
    s.displayMin +
    ((value - s.min) / Math.max(s.max - s.min, 1e-9)) *
      (s.displayMax - s.displayMin)
  );
}
export function unmapValue(value: number, segments: AxisSegment[]): number {
  if (!segments.length) return value;
  const s =
    segments.find((x) => value >= x.displayMin && value <= x.displayMax) ??
    segments[segments.length - 1];
  return (
    s.min +
    ((value - s.displayMin) / Math.max(s.displayMax - s.displayMin, 1e-9)) *
      (s.max - s.min)
  );
}
