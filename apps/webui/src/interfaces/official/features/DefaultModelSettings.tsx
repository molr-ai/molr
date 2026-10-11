import { Dialog } from '@molr/ui';
import type { Model, Settings } from '../../../api/types';
import { useErrorToast } from '../../../error-toasts';
import { useCopy, useQuery } from '../../../state';
import { ModelSelect } from '../components/ModelSelect';
import { Loading } from '../components/common';
import { useSettingsChange } from './use-settings-change';

export function DefaultModelSettings({ models, loading, onClose }: { models: Model[]; loading: boolean; onClose: () => void }) {
  const { t } = useCopy();
  const query = useQuery<Settings>('/api/settings');
  useErrorToast(query.error, query.reload);
  return <Dialog open title={t.resources.defaultModel} description={t.resources.defaultModelHelp} onOpenChange={open => { if (!open) onClose(); }}>
    {query.data ? <DefaultModelControl settings={query.data} models={models} loading={loading} /> : query.loading ? <Loading /> : null}
  </Dialog>;
}

function DefaultModelControl({ settings, models, loading }: { settings: Settings; models: Model[]; loading: boolean }) {
  const { t } = useCopy();
  const { value, change, action } = useSettingsChange(settings);
  useErrorToast(action.error);
  return <ModelSelect label={t.resources.defaultModel} size="sm" models={models} value={value.DefaultModelID} allowNone disabled={loading || action.busy} onValueChange={DefaultModelID => change({ DefaultModelID })} />;
}
