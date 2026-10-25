import test from 'node:test';
import assert from 'node:assert/strict';
import { RuntimeSession } from '../dist/main/main/runtime-session.js';

function fixture() {
  const destroyed = [];
  let cancellations = 0, noticesDisposed = 0;
  const session = new RuntimeSession(() => cancellations++, view => destroyed.push(view));
  const notices = () => ({ dispose: () => noticesDisposed++ });
  return { session, notices, destroyed, cancelled: () => cancellations, disposed: () => noticesDisposed };
}

test('handshake requires both signals and stale views cannot complete a new connection', t => {
  const { session, notices, destroyed } = fixture();
  t.after(() => session.dispose());
  const old = {}, current = {};
  session.attach(old, notices(), true);
  session.didHandshake();
  assert.equal(session.complete, false);
  session.didLoad(old);
  assert.equal(session.complete, true);
  assert.equal(session.consumeInitialLogin(), true);
  assert.equal(session.consumeInitialLogin(), false);
  session.attach(current, notices(), false);
  assert.deepEqual(destroyed, [old]);
  session.didLoad(old);
  session.didHandshake();
  assert.equal(session.complete, false);
  session.didLoad(current);
  assert.equal(session.complete, true);
  assert.equal(session.consumeInitialLogin(), false);
});

test('navigation and disposal own timeout, notification and OAuth cleanup', t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const f = fixture(), first = {}, second = {};
  t.after(() => f.session.dispose());
  let timeouts = 0;
  f.session.attach(first, f.notices(), true);
  const cancelled = f.cancelled(), disposed = f.disposed();
  f.session.navigate(first, false, () => timeouts++);
  assert.equal(f.cancelled(), cancelled + 1);
  assert.equal(f.disposed(), disposed + 1);
  assert.equal(f.session.consumeInitialLogin(), false);
  f.session.attach(second, f.notices(), true);
  t.mock.timers.tick(30_001);
  assert.equal(timeouts, 0);
  f.session.navigate(first, false, () => timeouts++);
  assert.equal(f.session.consumeInitialLogin(), true);
  f.session.navigate(second, false, () => timeouts++);
  t.mock.timers.tick(30_001);
  assert.equal(timeouts, 1);
  f.session.dispose();
  f.session.dispose();
  assert.deepEqual(f.destroyed, [first, second]);
  assert.equal(f.session.complete, false);
});

test('revealing a ready session cancels its deadline in either handshake order', t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const { session, notices } = fixture(), view = {};
  t.after(() => session.dispose());
  session.attach(view, notices(), false);
  session.navigate(view, false, () => assert.fail('ready session timed out'));
  session.didLoad(view);
  assert.equal(session.complete, false);
  session.didHandshake();
  assert.equal(session.complete, true);
  session.reveal();
  t.mock.timers.tick(30_001);
});
