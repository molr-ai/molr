const roles = ['system', 'tool', 'assistant', 'user'] as const;

function textLength(value: unknown): number {
  if (typeof value !== 'string') return 0;
  let length = 0;
  for (const _character of value) length++;
  return length;
}

// Arguments are already decoded by the Runtime JSON response. Count their
// values, not JSON keys, delimiters, indentation or escape sequences.
function argumentLength(value: unknown): number {
  if (typeof value === 'string') return textLength(value);
  if (typeof value === 'number' || typeof value === 'boolean') return String(value).length;
  if (Array.isArray(value)) return value.reduce((sum, item) => sum + argumentLength(item), 0);
  if (value && typeof value === 'object') return argumentLength(Object.values(value));
  return 0;
}

export function contextRoleShares(inputs: ReadonlyArray<Record<string, unknown>>) {
  const counts = { system: 0, assistant: 0, tool: 0, user: 0 };
  for (const input of inputs) {
    const role = roles.find(role => role === input.Role);
    if (!role) continue;
    counts[role] += textLength(input.Text);
    if (Array.isArray(input.Calls)) for (const call of input.Calls) {
      if (call && typeof call === 'object') counts[role] += textLength(call.Name) + argumentLength(call.Arguments);
    }
  }
  const total = Object.values(counts).reduce((sum, count) => sum + count, 0);
  return roles.map(role => ({ role, characters: counts[role], percentage: total ? counts[role] / total * 100 : 0 }));
}
