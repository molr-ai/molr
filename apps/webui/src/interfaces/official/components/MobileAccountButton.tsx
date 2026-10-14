import { UpdateBadge } from '../../../updates/provider';
import { Avatar } from '@molr/ui';
import { useCopy, useSession } from '../../../state';
import { useNavigation } from '../../../navigation';

export function MobileAccountButton() {
  const { t } = useCopy(), { principal } = useSession(), { navigate } = useNavigation();
  const name = principal?.User?.DisplayName || principal?.User?.Username || 'U';
  return <button type="button" className="mobile-account-button" aria-label={`${t.account.title} · ${name}`} onClick={() => navigate('account')}>
    <Avatar name={name} avatar={principal?.User?.Avatar} size={36} notice={<UpdateBadge compact />} />
    <span className="mobile-account-name">{name}</span>
  </button>;
}
