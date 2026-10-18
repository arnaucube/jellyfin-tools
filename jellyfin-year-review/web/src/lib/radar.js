// Use the same zero-based scale as the bar chart, with room for sparse datasets.
export function radarGeometry(values) {
  const max = Math.max(1, ...values);
  const axisCount = Math.max(3, values.length);
  const axes = Array.from({ length: axisCount }, (_, index) => {
    const angle = index * 2 * Math.PI / axisCount - Math.PI / 2;
    const x = Math.cos(angle), y = Math.sin(angle);
    const fraction = Math.max(0, values[index] ?? 0) / max;
    return { x, y, valueX: x * fraction, valueY: y * fraction };
  });
  return { max, axes };
}
