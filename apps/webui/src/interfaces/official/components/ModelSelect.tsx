import { useMemo } from 'react';
import { Select } from '@molr/ui';
import type { SelectOption } from '@molr/ui';
import type { Model, Resource } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { ErrorNotice } from './common';

const MODEL_SEARCH_THRESHOLD = 8;

export function ModelSelect({ models, value, onValueChange, allowNone = false, requireTools = true, size = 'md', showProvider = false }: {
  models: Model[]; value: string; onValueChange: (value: string) => void; allowNone?: boolean; requireTools?: boolean; size?: 'sm' | 'md'; showProvider?: boolean;
}) {
  const { t, locale } = useCopy();
  const providers = useQuery<Resource[]>('/api/resources?kind=model', true);
  const options = useMemo(() => {
    const names = new Map((providers.data ?? []).map(provider => [provider.ID, provider.Name]));
    const result: SelectOption[] = [...models].sort((a, b) =>
      (names.get(a.ResourceID) ?? '').localeCompare(names.get(b.ResourceID) ?? '', locale) || a.ResourceID.localeCompare(b.ResourceID) || a.Name.localeCompare(b.Name, locale)
    ).map(model => ({ value: model.ID, label: model.Name, disabled: (requireTools && !model.SupportsTools) || !names.has(model.ResourceID),
      group: { value: model.ResourceID, label: names.get(model.ResourceID) ?? t.resources.unknownProvider },
    }));
    if (allowNone) result.unshift({ value: '', label: t.none });
    if (value && !models.some(model => model.ID === value)) result.unshift({ value, label: t.resources.modelUnavailable, disabled: true });
    return result;
  }, [models, providers.data, value, allowNone, requireTools, locale, t]);
  const selectedProvider = providers.data?.find(provider => provider.ID === models.find(model => model.ID === value)?.ResourceID);
  return <>
    <Select label={t.assistants.model} size={size} value={value} onValueChange={onValueChange} options={options} description={showProvider && selectedProvider ? `${t.resourcePermissions.provider} · ${selectedProvider.Name}` : undefined}
      searchable={models.length >= MODEL_SEARCH_THRESHOLD} searchPlaceholder={t.resources.searchModels}
      disabled={!!providers.error || (!providers.data && providers.loading)} />
    <ErrorNotice error={providers.error} retry={providers.reload} />
  </>;
}
