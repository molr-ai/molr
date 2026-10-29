import type { GPUDevice } from '@molr/sdk';

/** UUIDs remain the authorization identity; names are presentation only. */
export function gpuName(id: string, devices: readonly GPUDevice[], locale: string): string {
  const device = devices.find(item => item.ID === id);
  const shortID = id.replace(/^GPU-/, '').slice(-8);
  if (!device?.Name) return `${locale === 'zh' ? '未识别的 GPU' : 'Unknown GPU'} · ${shortID}`;
  const memory = device.MemoryBytes > 0
    ? ` · ${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(device.MemoryBytes / 1073741824)} GiB`
    : '';
  const duplicates = devices.filter(item => item.Name === device.Name);
  // Extend the suffix when otherwise identical boards have colliding short IDs.
  let length = 8;
  while (length < id.length && duplicates.some(item => item.ID !== id && item.ID.slice(-length) === id.slice(-length))) length++;
  const identity = duplicates.length > 1 ? ` · ${id.slice(-length)}` : '';
  return `${device.Name}${memory}${identity}`;
}
