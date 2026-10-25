import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

async function moduleURL(path, replacements = []) {
  let source = await readFile(new URL(path, import.meta.url), 'utf8');
  for (const [from, to] of replacements) source = source.replaceAll(from, to);
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
  return `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`;
}
const sdkURL = await moduleURL('../../../packages/sdk/src/client.ts');
const { api, APIError } = await import(sdkURL);
const { createMutationOperation } = await import(await moduleURL('../src/mutation-operation.ts', [['@molr/sdk', sdkURL]]));

for (const status of [0, 200, 408, 429, 500, 503]) {
  test(`operation retains its key after an uncertain or retryable HTTP ${status} outcome`, async () => {
    const keys = [], mutate = createMutationOperation();
    let failing = true;
    api.mutate = async (_path, _body, options) => {
      keys.push(options.idempotencyKey);
      if (failing) throw new APIError('unavailable', '', status);
      return { ID: 'confirmed' };
    };
    const body = { Password: 'synthetic-password-2026', Text: 'x'.repeat(65536) };
    await assert.rejects(mutate('/api/admin/users', body));
    failing = false;
    assert.deepEqual(await mutate('/api/admin/users', body), { ID: 'confirmed' });
    await mutate('/api/admin/users', body);
    assert.equal(keys[0], keys[1]);
    assert.notEqual(keys[1], keys[2], 'Confirmed completion releases the operation key');
  });
}

test('definite rejection, version change, target change and new operation each use a new key', async () => {
  const keys = [], mutate = createMutationOperation();
  let status = 400;
  api.mutate = async (_path, _body, options) => { keys.push(options.idempotencyKey); throw new APIError('failure', '', status); };
  await assert.rejects(mutate('/api/example', {}, 1, 'first'));
  status = 503;
  await assert.rejects(mutate('/api/example', {}, 1, 'first'));
  await assert.rejects(mutate('/api/example', {}, 2, 'first'));
  await assert.rejects(mutate('/api/example', {}, 2, 'second'));
  await assert.rejects(createMutationOperation()('/api/example', {}, 2, 'second'));
  assert.equal(new Set(keys).size, keys.length);
});

test('an older completion cannot clear a newer pending operation', async () => {
  const pending = [], keys = [], mutate = createMutationOperation();
  api.mutate = (_path, _body, options) => {
    keys.push(options.idempotencyKey);
    return new Promise((resolve, reject) => pending.push({ resolve, reject }));
  };
  const older = mutate('/api/example', {}, 1, 'first');
  const newer = mutate('/api/example', {}, 1, 'second');
  const rejected = assert.rejects(newer);
  pending[1].reject(new APIError('unavailable', '', 503));
  await rejected;
  pending[0].resolve({});
  await older;
  const retry = mutate('/api/example', {}, 1, 'second');
  pending[2].resolve({});
  await retry;
  assert.notEqual(keys[0], keys[1]);
  assert.equal(keys[1], keys[2]);
});
