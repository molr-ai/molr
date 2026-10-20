import assert from 'node:assert/strict';
import { test } from 'node:test';
import { applicationMenu } from '../dist/main/main/menu.js';

for (const locale of ['zh', 'en']) {
  test(`${locale} menu translates native roles and preserves connection actions`, () => {
    const calls = [];
    const menu = applicationMenu(locale, { manager: () => calls.push('manager'), reconnect: () => calls.push('reconnect') });
    assert.deepEqual(menu.map(item => item.label), locale === 'zh' ? ['Molr', '连接', '编辑', '显示'] : ['Molr', 'Connections', 'Edit', 'View']);
    const walk = items => items.flatMap(item => [item, ...(Array.isArray(item.submenu) ? walk(item.submenu) : [])]);
    const items = walk(menu);
    for (const item of items.filter(item => item.type !== 'separator')) {
      assert.ok(item.label);
      if (item.label !== 'Molr') assert.equal(/[\u3400-\u9fff]/u.test(item.label), locale === 'zh', item.label);
    }
    for (const role of ['about', 'services', 'hide', 'hideOthers', 'unhide', 'quit', 'undo', 'redo', 'cut', 'copy', 'paste', 'selectAll', 'resetZoom', 'zoomIn', 'zoomOut', 'togglefullscreen']) assert.ok(items.some(item => item.role === role), role);
    menu[1].submenu[0].click(); menu[1].submenu[1].click();
    assert.deepEqual(calls, ['manager', 'reconnect']);
    assert.equal(menu[1].submenu[0].accelerator, 'CmdOrCtrl+Shift+M');
    assert.equal(menu[1].submenu[1].accelerator, 'CmdOrCtrl+R');
  });
}
