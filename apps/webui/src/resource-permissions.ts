import type { Grant, Model, Resource } from '@molr/sdk';
import type { Copy } from './messages';

/** Resource capabilities come from Runtime; these labels only describe them. */
export function permissionLabel(t: Copy, kind: Resource['Kind'] | undefined, action: string): string {
  const labels: Partial<Record<string, string>> = kind ? t.resourcePermissions.labels[kind] : {};
  const fallback: Partial<Record<string, string>> = t.resourcePermissions.generic;
  return labels[action] ?? fallback[action] ?? action;
}

export function permissionOptions(t: Copy, resource?: Resource) {
  return [...new Set(resource?.Actions ?? [])].map(value => ({ value, label: permissionLabel(t, resource?.Kind, value) }));
}

export function defaultPermissions(resource?: Resource): string[] {
  return resource?.Actions.includes('read') ? ['read'] : [];
}

export function grantSummary(t: Copy, grant: Grant, resource?: Resource) {
  if (grant.Scope.FullAccess) return t.resourcePermissions.fullAccess;
  return grant.Actions.map(action => permissionLabel(t, resource?.Kind, action)).join(' · ');
}

export function grantTarget(t: Copy, grant: Grant, resource: Resource | undefined, models: Model[]) {
  if (resource?.Kind === 'model' || grant.Scope.ModelID) return models.find(model => model.ID === grant.Scope.ModelID)?.Name ?? t.resources.modelUnavailable;
  return resource?.Name ?? grant.ResourceID;
}

export function grantOptionLabel(t: Copy, grant: Grant, resource: Resource | undefined, models: Model[]) {
  const target = grantTarget(t, grant, resource, models);
  return `${resource?.Kind === 'model' ? `${resource.Name} · ` : ''}${target} · ${grantSummary(t, grant, resource)}`;
}
