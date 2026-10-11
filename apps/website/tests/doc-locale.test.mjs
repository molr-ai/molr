import assert from 'node:assert/strict';
import { test } from 'node:test';
import { docLocale, isDocPath, localizedDocPath } from '../src/doc-locale.ts';

test('documentation language is explicit and switching preserves the article', () => {
  for (const root of ['docs', 'dev-docs']) {
    assert.equal(docLocale(`/en/${root}/deployment/`), 'en');
    assert.equal(docLocale(`/zh/${root}/`), 'zh');
    assert.equal(docLocale(`/${root}/`), undefined);
    assert.equal(localizedDocPath(`/zh/${root}/deployment/`, 'en'), `/en/${root}/deployment/`);
    assert.equal(localizedDocPath(`/${root}/`, 'zh'), `/zh/${root}/`);
    assert(isDocPath(`/en/${root}`));
    assert(!isDocPath(`/en/${root}-other/`));
  }
  assert.equal(localizedDocPath('https://example.invalid/docs/', 'zh'), 'https://example.invalid/docs/');
  assert.equal(localizedDocPath('/', 'en'), '/');
  assert.equal(docLocale('/en/'), undefined);
});
