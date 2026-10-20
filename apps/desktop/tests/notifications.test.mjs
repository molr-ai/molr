import assert from 'node:assert/strict';
import { test } from 'node:test';
import { EventEmitter } from 'node:events';
import { DesktopNotifications } from '../dist/main/main/notifications.js';

const value = (tag = 'one', route = '#/assistants/assistant') => ({ tag, title: 'Fixture', body: 'A message', route });
function fixture(timeout) {
  const notices = [], clicks = [];
  const controller = new DesktopNotifications(content => {
    const notice = new EventEmitter();
    notice.content = content; notice.closed = 0;
    notice.show = () => {}; notice.close = () => { notice.closed++; };
    notices.push(notice); return notice;
  }, (...args) => clicks.push(args), timeout);
  controller.begin('login-one');
  return { controller, notices, clicks };
}
test('native delivery waits for acknowledgement and reports failures and timeouts', async () => {
  const { controller, notices } = fixture(20);
  const delivered = controller.show('login-one', value());
  notices[0].emit('show'); await delivered;
  const failed = controller.show('login-one', value('two'));
  notices[1].emit('failed', {}, 'Permission denied');
  await assert.rejects(failed, /could not be delivered/);
  assert.equal(notices[1].closed, 1);
  await assert.rejects(controller.show('login-one', value('three')), /canceled|timed out/);
  assert.equal(notices[2].closed, 1); controller.dispose();
});
test('replacing tags and signing out cancel pending delivery and stale clicks', async () => {
  const { controller, notices, clicks } = fixture();
  const pending = controller.show('login-one', value());
  const canceled = assert.rejects(pending, /canceled/);
  const replacement = controller.show('login-one', value());
  notices[0].emit('click'); notices[0].emit('show');
  assert.deepEqual(clicks, []);
  notices[1].emit('show'); await replacement; await canceled;
  // Platform dismissal must not discard the handle required for logout cleanup.
  notices[1].emit('close');
  controller.begin('login-two');
  assert.equal(notices[1].closed, 1);
  notices[1].emit('click'); assert.deepEqual(clicks, []);
  await assert.rejects(controller.show('login-one', value()), /expired/);
  const fresh = controller.show('login-two', value()); notices[2].emit('show'); await fresh;
  controller.close('login-one'); assert.equal(notices[2].closed, 0);
  notices[2].emit('click'); assert.deepEqual(clicks, [['login-two', '#/assistants/assistant']]);
  assert.equal(notices[2].closed, 1); controller.dispose();
});
test('native input rejects external and malformed routes and oversized content', async () => {
  const { controller, notices } = fixture();
  for (const route of ['https://example.invalid', 'file:///tmp/private', '#/plans/../account', '#/assistants/a?redirect=bad']) {
    await assert.rejects(controller.show('login-one', value('one', route)), /Invalid/);
  }
  await assert.rejects(controller.show('login-one', { ...value(), body: 'x'.repeat(4097) }), /Invalid/);
  await assert.rejects(controller.show('login-one', { ...value(), tag: '' }), /Invalid/);
  assert.equal(notices.length, 0); controller.dispose();
});
test('disconnect cancels pending notifications and does not activate late OS events', async () => {
  const { controller, notices, clicks } = fixture();
  const pending = controller.show('login-one', value());
  controller.dispose(); await assert.rejects(pending, /canceled/);
  notices[0].emit('show'); notices[0].emit('click'); assert.deepEqual(clicks, []);
  assert.equal(notices[0].closed, 2);
});
