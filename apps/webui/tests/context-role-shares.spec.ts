import { test, expect } from '@playwright/test';
import { contextRoleShares } from '../src/interfaces/official/features/context-role-shares';

test('role shares count decoded content without serialization or metadata overhead', () => {
  const shares = contextRoleShares([
    { Role: 'system', Text: '指令', FileIDs: ['ignored'], ProviderState: { Payload: 'ignored' } },
    { Role: 'assistant', Text: '', Calls: [{ ID: 'ignored', Name: 'read', Arguments: { veryLongKey: ['中\n"😀', 12, true, null] } }] },
    { Role: 'tool', Text: '结果', ReplyTo: 'ignored' },
    { Role: 'user', Text: '😀' },
    { Role: 'user', Text: '\n' },
    { Role: 'unknown', Text: 'ignored' },
  ]);
  expect(shares.map(({ characters }) => characters)).toEqual([2, 2, 14, 2]);
  expect(shares.map(({ percentage }) => percentage)).toEqual([10, 10, 70, 10]);
});

test('missing roles and empty content have finite zero shares', () => {
  expect(contextRoleShares([]).map(item => item.percentage)).toEqual([0, 0, 0, 0]);
  expect(contextRoleShares([{ Role: 'tool', Text: 'result' }]).map(item => item.percentage)).toEqual([0, 100, 0, 0]);
  expect(contextRoleShares([{ Role: 'assistant', Calls: null }]).every(item => item.characters === 0)).toBe(true);
});
