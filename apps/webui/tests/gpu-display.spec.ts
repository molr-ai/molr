import { test, expect } from '@playwright/test';
import { gpuName } from '../src/gpu';
import type { GPUDevice } from '@molr/sdk';

const device = (ID: string): GPUDevice => ({ ID, Name: 'NVIDIA Example GPU', Vendor: 'nvidia', MemoryBytes: 24 * 1073741824, Available: true, Reason: 'available' });
test('GPU labels identify the model and distinguish identical boards without exposing long UUIDs', () => {
  const first = device('GPU-00000001-0000-0000-0000-000000000001');
  const second = device('GPU-00000002-0000-0000-0000-000000000001');
  expect(gpuName(first.ID, [first], 'en')).toBe('NVIDIA Example GPU · 24 GiB');
  const labels = [first, second].map(item => gpuName(item.ID, [first, second], 'zh'));
  expect(labels[0]).not.toBe(labels[1]);
  expect(labels.every(label => label.includes('NVIDIA Example GPU') && !label.includes('GPU-'))).toBe(true);
  expect(gpuName('GPU-missing-device', [], 'zh')).toContain('未识别的 GPU');
});
