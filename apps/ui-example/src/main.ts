import { connectUI } from '@molr/sdk/host';
import { APIError } from '@molr/sdk';
import type { Assistant, Message } from '@molr/sdk';
import type { ChatDraft } from '@molr/sdk/ui';
import './style.css';
import manifest from '../manifest.json';

const root = document.querySelector<HTMLElement>('#app')!;
if (window.parent === window) {
  root.innerHTML = `<header><span class="eyebrow">MOLR / FOCUS</span><h1>A different view of your conversations.</h1><p>这是可单独替换聊天页面的 UI 示例，无需 React 或官方组件库。</p></header><p>这是开发参考示例。产品目前仅提供内置主题，自定义主题暂不开放。</p><p><a href="/manifest.json" download="focus-ui.json">下载界面清单 / Download manifest</a></p>`;
  const entry = new URL(import.meta.env.BASE_URL, location.href).href;
  const link = root.querySelector<HTMLAnchorElement>('a')!;
  const download = URL.createObjectURL(new Blob([JSON.stringify({ ...manifest, pages: { assistants: entry } }, null, 2)], { type: 'application/json' }));
  link.href = download;
  window.addEventListener('pagehide', () => URL.revokeObjectURL(download), { once: true });
} else {
root.innerHTML = `<header><span class="eyebrow">MOLR / FOCUS</span><h1>One conversation at a time.</h1><p>A custom page. The same assistants.</p></header><label>Assistant<select id="assistant"><option value="">Choose an assistant</option></select></label><div id="messages" role="log" aria-label="Messages"></div><form><label>Message<textarea rows="3" placeholder="What would you like to do?"></textarea></label><footer><span id="status" role="status"></span><button type="submit">Send</button></footer></form>`;
const status = document.querySelector<HTMLElement>('#status')!, select = document.querySelector<HTMLSelectElement>('select')!, input = document.querySelector<HTMLTextAreaElement>('textarea')!, form = document.querySelector<HTMLFormElement>('form')!, send = document.querySelector<HTMLButtonElement>('button')!;
let assistants: Assistant[] = [], selected = '', draft: ChatDraft = { text: '', files: [] }, revision = 0;
function report(error: unknown) { status.textContent = error instanceof APIError ? error.code : error instanceof Error ? error.message : String(error); }
try {
  const host = await connectUI();
  document.documentElement.dataset.theme = host.context.theme;
  async function messages() {
    const assistant = assistants.find(a => a.ID === selected); if (!assistant) return;
    const sequence = ++revision;
    const values = await host.api.all<Message>(`/api/conversations/${assistant.ConversationID}/messages`);
    if (sequence !== revision) return;
    const list = document.querySelector<HTMLElement>('#messages')!; list.replaceChildren();
    for (const message of values) {
      const article = document.createElement('article'); article.dataset.sender = message.Sender;
      const who = document.createElement('small'); who.textContent = message.Sender === 'user' ? 'You' : assistant.Name;
      const text = document.createElement('p'); text.textContent = message.Content.Text;
      article.append(who, text); list.append(article);
    }
  }
  async function loadSelection(id: string) {
    selected = id; select.value = id; const expected = id;
    if (id) { const value = await host.drafts.get(id); if (selected !== expected) return; draft = value; input.value = draft.text; }
    else { draft = { text: '', files: [] }; input.value = ''; }
    input.disabled = send.disabled = !selected;
    await messages();
  }
  async function refresh() {
    assistants = (await host.api.all<Assistant>('/api/assistants')).filter(a => a.State === 'active');
    select.replaceChildren(new Option('Choose an assistant', ''), ...assistants.map(a => new Option(a.Name, a.ID)));
    select.value = selected;
    if (!selected || !assistants.some(a => a.ID === selected)) await loadSelection(assistants.find(a => a.ID === host.context.route.id)?.ID || assistants[0]?.ID || '');
    else await messages();
    if (!assistants.length) status.textContent = 'Create an assistant in the built-in interface to start.';
  }
  select.onchange = () => { void host.navigate('assistants', select.value).catch(report); };
  input.oninput = () => { draft = { ...draft, text: input.value }; void host.drafts.set(selected, draft).catch(report); };
  form.onsubmit = event => {
    event.preventDefault(); const assistant = assistants.find(a => a.ID === selected); if (!assistant || !input.value.trim()) return;
    const outgoing = { ...draft, text: input.value }, assistantID = selected; send.disabled = true;
    void host.api.mutate('/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: outgoing.text, AttachmentIDs: outgoing.files.map(file => file.ID) } }).then(async () => {
      if (selected === assistantID && input.value === outgoing.text) { draft = { text: '', files: [] }; input.value = ''; await host.drafts.set(assistantID, draft); }
      status.textContent = 'Sent'; await messages();
    }).catch(report).finally(() => { send.disabled = !selected; });
  };
  host.onContext(context => { document.documentElement.dataset.theme = context.theme; if (context.route.id !== selected) void loadSelection(context.route.id || assistants[0]?.ID || '').catch(report); });
  let refreshing = false;
  host.onDataChange(() => { if (!refreshing) { refreshing = true; void refresh().catch(report).finally(() => { refreshing = false; }); } });
  window.addEventListener('pagehide', host.dispose, { once: true });
  await refresh();
} catch (error) { report(error); input.disabled = send.disabled = true; }

}
