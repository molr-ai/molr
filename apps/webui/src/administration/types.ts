import type { ResourceQuota, UserGroup } from '@molr/sdk';

export const emptyQuota = (): ResourceQuota => ({ Calls: 0, Concurrent: 0, Period: 'month', Timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' });
export const groupName = (group: Pick<UserGroup, 'ID' | 'Name'>, zh: boolean) => group.ID === 'administrators' ? (zh ? '管理员' : 'Administrators') : group.Name === 'Users' && group.ID === 'users' ? (zh ? '普通用户' : 'Users') : group.Name;
