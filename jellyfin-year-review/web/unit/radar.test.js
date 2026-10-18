import { test } from 'node:test';
import assert from 'node:assert/strict';
import { radarGeometry } from '../src/lib/radar.js';

test('radar preserves genre order and scales watch time from zero to the largest value', () => {
  const values = [20, 10, 5, 0];
  const { max, axes } = radarGeometry(values);
  assert.equal(max, 20);
  for (let i = 0; i < values.length; i++) {
    assert.ok(Math.abs(Math.hypot(axes[i].valueX, axes[i].valueY) - values[i] / 20) < 1e-10);
  }
  assert.ok(axes[0].y < 0 && Math.abs(axes[0].x) < 1e-10);
  assert.ok(axes[1].x > 0 && Math.abs(axes[1].y) < 1e-10);
});

test('empty, zero and sparse data keep the radar grid and points finite', () => {
  for (const values of [[], [0, 0, 0], [4], [4, 2]]) {
    const { max, axes } = radarGeometry(values);
    assert.ok(max > 0);
    assert.ok(axes.length >= 3);
    assert.ok(axes.every(axis => Object.values(axis).every(Number.isFinite)));
    assert.ok(axes.slice(values.length).every(axis => axis.valueX === 0 && axis.valueY === 0));
  }
});
