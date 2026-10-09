// Deterministic provider rejection scenarios, shared by E2E and acceptance.
const attempts = new Map();
export function retryModel(input, response) {
  if (!input.model?.startsWith('Retry ')) return false;
  const key = JSON.stringify(input), count = (attempts.get(key) ?? 0) + 1;
  attempts.set(key, count);
  const messages = input.messages ?? [];
  const eventIndex = messages.findLastIndex(message => typeof message.content === 'string' && message.content.startsWith('Runtime event: '));
  const hasTool = messages.slice(eventIndex + 1).some(message => message.role === 'tool');
  if (input.model.startsWith('Retry unknown')) {
    response.writeHead(504, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ error: { message: 'Unknown upstream result.' } })); return true;
  }
  if (input.model.startsWith('Retry quota')) {
    response.writeHead(429, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ error: { code: 'insufficient_quota', message: 'Quota is exhausted.' } })); return true;
  }
  if (!hasTool && (count <= 2 || input.model.startsWith('Retry exhausted'))) {
    response.writeHead(429, { 'Content-Type': 'application/json', 'Retry-After': '2' });
    response.end(JSON.stringify({ error: { code: 'rate_limit_exceeded', message: `Retry fixture attempt ${count}` } })); return true;
  }
  return false;
}
