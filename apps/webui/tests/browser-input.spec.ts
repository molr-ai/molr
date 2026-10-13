import { test, expect } from '@playwright/test';
import { BrowserInputBuffer } from '../src/interfaces/official/features/browser-input';

test('input coalescing preserves barriers, scroll direction, drag endpoints and text events', () => {
  const buffer = new BrowserInputBuffer();
  const move = (x: number) => ({ action: 'pointer', event: 'move', x, y: 20, modifiers: 0 });
  for (let i = 0; i < 1000; i++) expect(buffer.push(move(i))).toBe(true);
  buffer.push({ action: 'pointer', event: 'down', x: 999 });
  for (let i = 0; i < 100; i++) buffer.push(move(i), true, i * 40);
  buffer.push({ action: 'pointer', event: 'up', x: 99 });
  const drag = buffer.take();
  expect(drag[0].x).toBe(999); expect(drag[1].event).toBe('down');
  expect(drag[2].x).toBe(0); expect(drag.at(-2)?.x).toBe(99); expect(drag.at(-1)?.event).toBe('up');
  expect(drag.length).toBeLessThanOrEqual(11);
  const wheel = (delta: number) => ({ action: 'wheel', x: 1, y: 2, delta_x: 0, delta_y: delta, modifiers: 0 });
  for (let i = 0; i < 100; i++) buffer.push(wheel(0.5));
  buffer.push(wheel(-5)); buffer.push({ action: 'insert_text', text: 'a' }); buffer.push({ action: 'press', key: 'Tab' }); buffer.push({ action: 'insert_text', text: '中文' });
  const actions = buffer.take();
  expect(actions.map(a => a.action)).toEqual(['wheel', 'wheel', 'insert_text', 'press', 'insert_text']);
  expect(actions[0].delta_y).toBe(50); expect(actions[1].delta_y).toBe(-5);
  buffer.prepend(actions.slice(2)); expect(buffer.take()).toEqual(actions.slice(2));
});

test('batches stay within wire limits and navigation stays ordered', () => {
  const buffer = new BrowserInputBuffer();
  for (let i = 0; i < 100; i++) buffer.push({ action: 'insert_text', text: String(i) });
  buffer.push({ action: 'select_tab', tab: 't2' }); buffer.push({ action: 'insert_text', text: 'next' });
  const counts = [buffer.take().length, buffer.take().length, buffer.take().length, buffer.take().length];
  expect(counts).toEqual([32, 32, 32, 4]);
  expect(buffer.take()).toEqual([{ action: 'select_tab', tab: 't2' }]);
  expect(buffer.take()).toEqual([{ action: 'insert_text', text: 'next' }]);
  buffer.push({ action: 'insert_text', text: '中'.repeat(2600) }); buffer.push({ action: 'insert_text', text: '文'.repeat(2600) });
  expect(buffer.take()).toHaveLength(1); expect(buffer.take()).toHaveLength(1);
  expect(buffer.push({ action: 'insert_text', text: '中'.repeat(3000) })).toBe(false);
});
