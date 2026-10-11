import { useErrorToast } from '../error-toasts';
import { useState } from 'react';
import { Badge, Button, TextField } from '@molr/ui';
import { AssistantAvatarField } from '../interfaces/official/features/AssistantAvatarField';
import { api } from '../api/client';
import { useCopy, useQuery, useSession } from '../state';
import { Loading, useAction } from '../interfaces/official/components/common';
import type { UserInfo } from './types';
import { groupName } from './types';

export function ProfileSettings() {
  const query = useQuery<UserInfo>('/api/account');
  useErrorToast(query.error, query.reload);
  if (!query.data) return <><Loading /></>;
  return <ProfileForm key={query.data.ID} user={query.data} />;
}
function ProfileForm({ user }: { user: UserInfo }) {
  const { locale, t } = useCopy(), zh = locale === 'zh', { refreshProfile } = useSession(), action = useAction();
  const [name, setName] = useState(user.DisplayName), [avatar, setAvatar] = useState(user.Avatar ?? '');
  const [processingAvatar, setProcessingAvatar] = useState(false);
  const [previous, setPrevious] = useState(user);
  if (user.Version > previous.Version) {
    setPrevious(user);
    if (name === previous.DisplayName) setName(user.DisplayName);
    if (avatar === (previous.Avatar ?? '')) setAvatar(user.Avatar ?? '');
  }
  useErrorToast(action.error);
  return <form className="editor settings-form" onSubmit={e => {
    e.preventDefault();
    if (processingAvatar) return;
    void action.run(async () => {
      const saved = await api.mutate<UserInfo>('/api/account/profile', { DisplayName: name, Avatar: avatar }, previous.Version);
      setPrevious(saved); setName(saved.DisplayName); setAvatar(saved.Avatar ?? '');
      await refreshProfile();
    });
  }}>
    <fieldset className="editor profile-fields" disabled={action.busy}>
      <AssistantAvatarField name={name || user.Username} value={avatar} onChange={setAvatar} onBusyChange={setProcessingAvatar} />
      <TextField label={zh ? '用户名' : 'Username'} value={user.Username} disabled />
      <TextField label={zh ? '显示名称' : 'Display name'} value={name} onChange={e => setName(e.target.value)} required maxLength={128} />
      <p>{zh ? '用户组' : 'User group'}：<Badge>{groupName({ ID: user.GroupID, Name: user.GroupName }, zh)}</Badge></p>
    </fieldset>
    <Button type="submit" loading={action.busy} disabled={processingAvatar}>{t.save}</Button>
  </form>;
}
