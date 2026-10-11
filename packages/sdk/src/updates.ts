import type { RuntimeClient } from './client';
import type { UpdateStatus } from './types';
// Accept the common client surface so hosts can retain their own transport policy.
export function updates(client: Pick<RuntimeClient, 'get' | 'post'>) {
 return { status: () => client.get<UpdateStatus>('/api/updates'), check: (automatic = false) => client.post<UpdateStatus>('/api/updates/check', { automatic }) };
}
