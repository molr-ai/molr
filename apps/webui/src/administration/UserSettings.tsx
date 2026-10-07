import { useState } from 'react';
import { Badge, Button, TextField } from '@molr/ui';
import { AssistantAvatarField } from '../interfaces/official/features/AssistantAvatarField';
import { api } from '../api/client';
import { useCopy, useQuery, useSession } from '../state';
import { ErrorNotice, Loading, useAction } from '../interfaces/official/components/common';
import type { UserInfo } from './types';
import { groupName } from './types';

export function ProfileSettings() {
  const query = useQuery<UserInfo>('/api/account');
  if (!query.data) return <><ErrorNotice error={query.error} retry={query.reload} /><Loading /></>;
  return <ProfileForm key={query.data.Version} user={query.data} />;
}
function ProfileForm({ user }: { user: UserInfo }) {
  const { locale, t } = useCopy(), zh = locale === 'zh', { reload } = useSession(), action = useAction();
  const [name, setName] = useState(user.DisplayName), [avatar, setAvatar] = useState(user.Avatar ?? '');
  const [processingAvatar, setProcessingAvatar] = useState(false);
  return <form className="editor settings-form" onSubmit={e => {
    e.preventDefault();
    if (processingAvatar) return;
    void action.run(async () => { await api.mutate('/api/account/profile', { DisplayName: name, Avatar: avatar }, user.Version); await reload(); });
  }}>
    <fieldset className="editor profile-fields" disabled={action.busy}>
      <AssistantAvatarField name={name || user.Username} value={avatar} onChange={setAvatar} onBusyChange={setProcessingAvatar} />
      <TextField label={zh ? '用户名' : 'Username'} value={user.Username} disabled />
      <TextField label={zh ? '显示名称' : 'Display name'} value={name} onChange={e => setName(e.target.value)} required maxLength={128} />
      <p>{zh ? '用户组' : 'User group'}：<Badge>{groupName({ ID: user.GroupID, Name: user.GroupName }, zh)}</Badge></p>
    </fieldset>
    <ErrorNotice error={action.error} /><Button type="submit" loading={action.busy} disabled={processingAvatar}>{t.save}</Button>
  </form>;
}
