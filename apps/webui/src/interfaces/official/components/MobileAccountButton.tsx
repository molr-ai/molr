import { UpdateBadge } from '../../../updates/provider';
import { AssistantAvatar } from '@molr/ui';
import { useCopy, useSession } from '../../../state';
import { useNavigation } from '../../../navigation';

export function MobileAccountButton() {
  const { t } = useCopy(), { principal } = useSession(), { navigate } = useNavigation();
  const name = principal?.User?.DisplayName || principal?.User?.Username || 'U';
  return <button type="button" className="mobile-account-button" aria-label={`${t.account.title} · ${name}`} onClick={() => navigate('account')}>
    <span className="user-avatar" aria-hidden="true"><AssistantAvatar name={name} avatar={principal?.User?.Avatar} /></span>
    <span className="mobile-account-name">{name}</span>
  <UpdateBadge compact /></button>;
}
