import type { Assistant, DelegationPermissionRequestInput, PermissionRequestInput } from '@molr/sdk';

export function isDelegationPermission(input: PermissionRequestInput): input is DelegationPermissionRequestInput {
  return input.permission_type === 'temporary_hiring' || input.permission_type === 'assistant_communication';
}
export function delegationConfiguration(assistant: Assistant, type: DelegationPermissionRequestInput['permission_type']): DelegationPermissionRequestInput | null {
  if (type === 'temporary_hiring') {
    const h = assistant.Hiring;
    return h?.Enabled ? { permission_type: type, hiring: { max_assistants: h.MaxAssistants, budget: h.Budget, grant_ids: h.GrantIDs }, expires_at: h.ExpiresAt, reason: '' } : null;
  }
  const c = assistant.PeerCommunication;
  return c?.Enabled ? { permission_type: type, communication: { all_assistants: c.AllAssistants, assistant_ids: c.AssistantIDs }, expires_at: c.ExpiresAt, reason: '' } : null;
}
