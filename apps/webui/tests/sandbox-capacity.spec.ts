import { readLayout } from '../../../scripts/test-support/layout.ts';
import { test, expect } from '@playwright/test';

// Deterministic capacity fixtures exercise presentation independently of CI hardware.
test('sandbox capacity uses hardware bounds, quotas and separate GPU lines', async ({ page }) => {
  let limited = true;
  await page.route(url => url.pathname === '/api/resources', async route => {
    const response = await route.fetch();
    const data = await response.json();
    const resources = data.Items ?? data;
    if (Array.isArray(resources)) for (const resource of resources) {
      if (resource.Provider === 'managed-workspace') resource.Name = limited ? 'Assistant workspace' : 'Project files';
    }
    await route.fulfill({ response, json: data });
  });
  await page.route(url => url.pathname === '/api/sandboxes', async route => {
    const response = await route.fetch();
    const sandboxes = await response.json();
    for (const sandbox of sandboxes) {
      sandbox.Info.HostCapacity = { CPUPercent: 800, MemoryBytes: 16 * 2 ** 30, Processes: 2048, CPUUsagePercent: limited ? 25 : 0, MemoryUsedBytes: limited ? 8 * 2 ** 30 : null };
      sandbox.Info.AuthorizationCeilings = limited
        ? { CPUPercent: 250, MemoryBytes: 32 * 2 ** 30, Processes: 512 }
        : { CPUPercent: 1000000, MemoryBytes: Number.MAX_SAFE_INTEGER, Processes: 2 ** 31 - 1 };
      sandbox.Info.StorageCeilingBytes = 8 * 2 ** 30;
      sandbox.Info.GPUs = [1, 2].map(index => ({ ID: `GPU-00000000-0000-0000-0000-00000000000${index}`, Name: 'Fixture GPU', MemoryBytes: 24 * 2 ** 30, Available: index === 1, UtilizationPercent: index === 1 ? 30 : null, MemoryUsedBytes: index === 1 ? 6 * 2 ** 30 : null }));
    }
    await route.fulfill({ response, json: sandboxes });
  });
  await page.route(url => url.pathname === '/api/sandboxes/local/resources', async route => {
    const response = await route.fetch();
    const details = await response.json();
    for (const detail of details) detail.Capacity = { StorageBytes: 64 * 2 ** 30, StorageAvailableBytes: 32 * 2 ** 30, StorageTotalBytes: 64 * 2 ** 30, StorageUsedBytes: 32 * 2 ** 30 };
    await route.fulfill({ response, json: details });
  });
  await page.goto('/#/resources');
  const username = page.getByRole('textbox', { name: 'Username', exact: true });
  // Each test starts with a fresh browser context; wait for login to render.
  await username.fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: /^(Sandboxes|沙盒)$/ }).click();
  await page.getByRole('tab', { name: /^(Storage|存储)$/ }).click();
  const summary = page.locator('.sandbox-compute-summary');
  await expect(summary).toContainText(/Available storage capacity|可使用的存储空间上限/);
  await expect(summary).toContainText('8 GiB');
  await expect(summary).toContainText(/Local workspace|本地工作区/);
  await expect(page.locator('.sandbox-resource-heading').filter({ hasText: /Local workspace|本地工作区/ })).toContainText(/Local workspace|本地工作区/);
  await expect(page.locator('.sandbox-section')).not.toContainText('Assistant workspace');
  await expect(summary.getByRole('progressbar', { name: /Disk usage|磁盘占用/ }).first()).toHaveAttribute('aria-valuenow', '50');
  await page.getByRole('tab', { name: /^(Compute|计算)$/ }).click();
  await expect(summary).toContainText(/2\.5 (cores|核) · 250%/);
  await expect(summary).toContainText('16 GiB');
  await expect(summary).toContainText('512');
  const usage = summary.getByRole('progressbar', { name: /Machine usage|机器占用/ });
  await expect(usage.nth(0)).toHaveAttribute('aria-valuenow', '25');
  await expect(usage.nth(1)).toHaveAttribute('aria-valuenow', '50');
  const gpus = summary.locator('.sandbox-gpu-list > .sandbox-gpu');
  await expect(gpus).toHaveCount(2);
  await expect(gpus.nth(1)).toContainText(/\(unavailable\)|（不可用）/);
  await expect(gpus.nth(0).getByRole('progressbar', { name: /^(GPU usage|GPU 占用)$/ })).toHaveAttribute('aria-valuenow', '30');
  await expect(gpus.nth(0).getByRole('progressbar', { name: /^(GPU memory usage|显存占用)$/ })).toHaveAttribute('aria-valuenow', '25');
  await expect(gpus.nth(1).getByRole('progressbar')).toHaveCount(0);
  const [first, second] = await readLayout(gpus.nth(0), gpus.nth(1));
  expect(first && second && second.y >= first.y + first.height).toBeTruthy();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  limited = false;
  await page.getByRole('button', { name: /^(Check again|重新检查)$/ }).click();
  await expect(summary).toContainText(/8 (cores|核) · 800%/);
  await expect(summary).toContainText('2,048');
  await expect(usage).toHaveCount(1);
  await expect(usage).toHaveAttribute('aria-valuenow', '0');
  await expect(summary).toContainText(/Usage unavailable|占用信息暂不可用/);
  await page.reload();
  await page.getByRole('tab', { name: /^(Sandboxes|沙盒)$/ }).click();
  await page.getByRole('tab', { name: /^(Storage|存储)$/ }).click();
  await expect(summary).toContainText('Project files');
});

test('Windows compute includes shared GPUs without a separate device grant', async ({ page }) => {
  await page.route(url => url.pathname === '/api/sandboxes', async route => {
    const response = await route.fetch();
    const sandboxes = await response.json();
    for (const sandbox of sandboxes) {
      sandbox.Info.HostCapacity = { CPUPercent: 2800, CPUUsagePercent: 10, MemoryBytes: 64 * 2 ** 30, MemoryUsedBytes: 16 * 2 ** 30, Processes: 0,
        GPUs: [{ ID: 'GPU-00000000-0000-0000-0000-000000000001', Name: 'NVIDIA Fixture', Vendor: 'nvidia', MemoryBytes: 16 * 2 ** 30, UtilizationPercent: 20, MemoryUsedBytes: 4 * 2 ** 30, Available: false, Reason: 'gpu_platform_unsupported' }] };
      sandbox.Info.Capabilities = { ...sandbox.Info.Capabilities, OS: 'windows', GPUAccessMode: 'shared', NetworkBackend: 'appcontainer-toggle', ResourceLimits: false };
      sandbox.Info.GPUs = [];
      sandbox.Info.AuthorizationCeilings = {};
    }
    await route.fulfill({ response, json: sandboxes });
  });
  await page.route(url => url.pathname === '/api/assistants', route => route.fulfill({ json: { Items: [{ ID: 'shared-gpu-assistant', Name: 'Shared GPU assistant', State: 'active', Version: 1 }], HasMore: false } }));
  await page.goto('/#/resources');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: /^(Sandboxes|沙盒)$/ }).click();
  await page.getByRole('tab', { name: /^(Compute|计算)$/ }).click();
  const summary = page.locator('.sandbox-compute-summary');
  await expect(summary).toContainText('64 GiB');
  await expect(summary).toContainText('NVIDIA Fixture');
  await expect(summary).toContainText(/shared with compute permission|随计算权限共享使用/);
  await expect(page.locator('.sandbox-resource-description')).toContainText(/all GPUs|全部 GPU/);
  await expect(summary.locator('.sandbox-gpu').getByRole('progressbar', { name: /^(GPU memory usage|显存占用)$/ })).toHaveAttribute('aria-valuenow', '25');
  await expect(page.getByRole('combobox', { name: /GPU devices|GPU 设备/ })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.getByRole('button', { name: /Grant assistant access|授权给助理/ }).first().click();
  await page.getByRole('dialog').getByRole('button', { name: /Grant assistant access|授权给助理/ }).click();
  const editor = page.getByRole('dialog').last();
  await expect(editor).toContainText(/all GPUs|全部 GPU/);
  await expect(editor.getByRole('combobox', { name: /GPU devices|GPU 设备/ })).toHaveCount(0);
  await expect(editor.getByRole('checkbox', { name: /No per-execution duration limit|不限制单次运行时长/ })).toBeVisible();
});
