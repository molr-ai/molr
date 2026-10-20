import test from 'node:test';
import assert from 'node:assert/strict';
import { localVersionState } from '../dist/main/runtime-version.js';

test('local update guidance follows SemVer precedence, not string order or app version', () => {
  for (const [installed, bundled] of [
    ['0.9.0','0.10.0'], ['1.0.0-rc.2','1.0.0-rc.10'], ['1.0.0-beta.9','1.0.0-rc.1'],
    ['1.0.0-rc.1','1.0.0'], ['0.1.0-local','0.2.0-local'],
    ['1.0.0-0.nightly.20261020.90000.gabcdef0','1.0.0-0.nightly.20261021.100.gabcdef0'],
    ['1.0.0-1','1.0.0-alpha'], ['1.0.0-alpha','1.0.0-alpha.1'],
    ['9007199254740992.0.0','9007199254740993.0.0'],
  ]) {
    assert.equal(localVersionState(installed,bundled),'update', `${installed} -> ${bundled}`);
    assert.equal(localVersionState(bundled,installed),'service-newer', `${bundled} -> ${installed}`);
  }
  assert.equal(localVersionState('0.1.0','0.1.0'),'same');
  assert.equal(localVersionState('0.1.0-local','0.1.0-local'),'same');
});
test('metadata, invalid versions and missing information never advertise an upgrade', () => {
  for (const [installed,bundled] of [
    ['1.0.0+one','1.0.0+two'], ['1.0.0+one','1.0.0'], ['1.0.0-rc.1+one','1.0.0-rc.1+two'],
    ['dev','1.0.0'], ['01.0.0','2.0.0'], ['1.0.0-rc.01','2.0.0'], ['1.0','2.0.0'],
  ]) assert.equal(localVersionState(installed,bundled),'different');
  assert.equal(localVersionState(undefined,'2.0.0'),'unknown');
  assert.equal(localVersionState('1.0.0',undefined),'unknown');
});
