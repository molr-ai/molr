import { noticeRoute, useNoticeText } from './system-copy';
import { createContext, useContext, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useToast } from '@molr/ui';
import type { Assistant, Conversation, Message, Page, SystemNotice } from '@molr/sdk';
import { api } from '../api/client';
import { useCopy, useData, useQuery, useSession } from '../state';
import { createBrowserNotifications } from './browser';
import { NotificationClaims } from './coordinator';
import { MessageSound } from './sound';
import type { SoundState } from './sound';
import { notificationCopy } from './copy';
import type { NotificationAdapter, NotificationCapability, NotificationPreferences, RuntimeEvent } from './types';

const defaults: NotificationPreferences = { enabled: false, preview: false, sound: false, system: true };
function preferences(key: string): NotificationPreferences {
  // Migrate the old DND preference to system notifications off, preserving sound.
  try { const value = JSON.parse(localStorage.getItem(key) ?? '{}'); return { enabled: value?.enabled === true, preview: value?.preview === true, sound: value?.sound === true, system: value?.system !== false && value?.quiet !== true }; } catch { return { ...defaults }; }
}
type Controls = { preferences: NotificationPreferences; change: (value: Partial<NotificationPreferences>) => void; capability: NotificationCapability; soundState: SoundState; testSound: () => Promise<boolean>; requestPermission: () => Promise<void>; test: () => Promise<boolean> };
const NotificationContext = createContext<Controls | null>(null);
export const useNotifications = () => useContext(NotificationContext)!;
const openRoute = (route: string) => { if (/^#\/(?:assistants(?:\/[A-Za-z0-9_-]{1,128}(?:\?panel=(?:info|permissions|channel|tasks)(?:&task=[A-Za-z0-9_-]{1,128})?)?)|plans\/[A-Za-z0-9_-]{1,128})$/.test(route)) location.hash = route; };
const focused = () => document.visibilityState === 'visible' && document.hasFocus();
const reading = (id: string) => focused() && location.hash === `#/assistants/${id}` && !document.querySelector('dialog[open]');

export function NotificationProvider({ children, createAdapter = createBrowserNotifications }: { children: ReactNode; createAdapter?: typeof createBrowserNotifications }) {
  const noticeText = useNoticeText();
  const account = useSession().principal!.OwnerID, key = `molr-notifications:${account}`;
  const conversations = useQuery<Conversation[]>('/api/conversations', true);
  const baseline = useRef<Map<string, number> | null>(null);
  useEffect(() => {
    // Capture only the first authorized snapshot. Updating this baseline on
    // every live refresh could suppress a new message before it is notified.
    if (!baseline.current && conversations.data) baseline.current = new Map(conversations.data.map(item => [item.ID, item.LastSequence]));
    if (!baseline.current && conversations.error) {
      const timer = setTimeout(conversations.reload, 5000);
      return () => clearTimeout(timer);
    }
  }, [conversations.data, conversations.error, conversations.reload]);
  const { subscribeEvents } = useData(), { locale } = useCopy(), toast = useToast();
  const [value, setValue] = useState(() => preferences(key)), [capability, setCapability] = useState<NotificationCapability>('unsupported');
  const [soundState, setSoundState] = useState<SoundState>('blocked');
  const current = useRef({ noticeText, value, copy: notificationCopy[locale], toast });
  current.current = { noticeText, value, copy: notificationCopy[locale], toast };
  const adapter = useRef<NotificationAdapter | null>(null), audio = useRef<MessageSound | null>(null);
  const applyPreferences = useRef<(previous: NotificationPreferences, next: NotificationPreferences) => void>(() => {});
  function change(patch: Partial<NotificationPreferences>) {
    const previous = current.current.value, next = { ...previous, ...patch };
    current.current.value = next; setValue(next);
    try { localStorage.setItem(key, JSON.stringify(next)); } catch { /* In-memory preference remains usable. */ }
    applyPreferences.current(previous, next);
    if (patch.sound === true) void audio.current?.unlock();
  }
  useEffect(() => {
    let noticeBaseline: Set<string> | null = null;
    let active = true;
    const controller = new AbortController(), claims = new NotificationClaims();
    const device = createAdapter(account, openRoute); adapter.current = device;
    const sound = new MessageSound(state => { if (active) setSoundState(state); }); audio.current = sound; setSoundState(sound.state());
    const toasts = new Map<string, number>(), pending = new Map<string, { assistant: Assistant; messages: Message[]; timer: ReturnType<typeof setTimeout> }>();
    const noticeGroups = new Map<string,string>();
    const suppressed = new Set<string>(), revisions = new Map<string, number>();
    let revision = 0;
    const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(`molr-notifications:${account}`);
    channel?.addEventListener('message', ({ data }) => {
      if (data?.type === 'reading' && typeof data.conversation === 'string') suppressed.add(data.conversation);
      if (data?.type === 'probe' && typeof data.assistant === 'string' && reading(data.assistant)) channel.postMessage({ type: 'reading', conversation: data.conversation });
    });
    function close(conversation?: string) {
      if (conversation) revisions.set(conversation, (revisions.get(conversation) ?? 0) + 1); else revision++;
      void device.close(conversation);
      for (const [id, entry] of toasts) if (!conversation || conversation === id) { current.current.toast.dismiss(entry); toasts.delete(id); }
      for (const [id, entry] of pending) if (!conversation || conversation === id) { clearTimeout(entry.timer); pending.delete(id); }
    }
    applyPreferences.current = (previous, next) => {
      if (!next.enabled || previous.preview && !next.preview) close();
      if (!next.enabled || !next.sound) sound.stop();
      // Disabling the system channel leaves sound and in-app alerts intact.
      if (!next.system) void device.close();
    };
    // Unlock after activation, so removing the blocked hint cannot move a
    // button between pointerdown and click and swallow the user's first click.
    const unlockSound = (event: Event) => { if (event.isTrusted && current.current.value.sound && sound.state() !== 'ready') void sound.unlock(); };
    window.addEventListener('click', unlockSound); window.addEventListener('keydown', unlockSound);
    function sync() { if (active) setCapability(device.capability()); }
    function storage(event: StorageEvent) { if (event.key === key || event.key === null) { const previous = current.current.value, next = preferences(key); current.current.value = next; setValue(next); applyPreferences.current(previous, next); } }
    window.addEventListener('focus', sync); document.addEventListener('visibilitychange', sync); window.addEventListener('storage', storage); sync();
    let retryNotices: ReturnType<typeof setTimeout> | undefined;
    function initializeNotices() {
      void api.all<SystemNotice>('/api/notifications?filter=active', controller.signal).then(items => {
        if (active) noticeBaseline = new Set(items.map(item => item.ID));
      }, () => { if (active) retryNotices = setTimeout(initializeNotices, 5000); });
    }
    initializeNotices();
    async function deliver(conversation: string) {
      const batch = pending.get(conversation); pending.delete(conversation);
      if (!batch || !active) return;
      const started = revision, conversationRevision = revisions.get(conversation);
      const canceled = () => !active || started !== revision || conversationRevision !== revisions.get(conversation);
      const { assistant } = batch;
      const message = batch.messages.sort((a, b) => a.Sequence - b.Sequence).at(-1)!;
      try {
        const conversationState = await api.get<Conversation>(`/api/conversations/${encodeURIComponent(conversation)}`, controller.signal);
        const prefs = current.current.value;
        if (canceled() || !prefs.enabled || conversationState.Archived || message.Sequence <= conversationState.LastReadSequence || reading(assistant.ID) || suppressed.has(conversation)) return;
        if (!await claims.claim(account, conversation, message.Sequence) || canceled() || !current.current.value.enabled || reading(assistant.ID) || suppressed.has(conversation)) return;
        const soundStarted = Date.now();
        if (current.current.value.sound && sound.state() === 'ready' && await claims.claimSound(account)) {
          if (!canceled() && current.current.value.enabled && current.current.value.sound && !reading(assistant.ID) && !suppressed.has(conversation) && Date.now() - soundStarted < 2000) sound.play();
        }
        if (canceled()) return;
        const { copy } = current.current, route = `#/assistants/${assistant.ID}`;
        const preview = current.current.value.preview;
        const title = preview ? assistant.Name : 'Molr';
        const body = preview ? (message.Content.Text || message.Content.Card?.Title || copy.newMessage).slice(0, 180) : copy.newMessage;
        // Foreground pages use in-app alerts; background pages prefer the OS.
        if (!focused() && current.current.value.system && device.capability() === 'granted') {
          try { await device.show({ tag: conversation, title, body, route, silent: true }); if (current.current.value.system) return; } catch { /* Use the same in-app fallback as unavailable environments. */ }
        }
        if (canceled()) return;
        const options = { title, description: body, duration: 8000, action: { label: copy.open, onClick: () => openRoute(route) } };
        const previous = toasts.get(conversation);
        if (previous) current.current.toast.dismiss(previous);
        toasts.set(conversation, current.current.toast.toast(options));
      } catch { /* Authorization and delivery failures must not expose stale content. */ }
    }
    async function deliverNotice(id:string) {
      const tag=`notice:${id}`, started=revision, localRevision=revisions.get(`notice:${id}`);
      const canceled=()=>!active||revision!==started||revisions.get(tag)!==localRevision;
      try {
        const n=await api.get<SystemNotice>(`/api/notifications/${id}`,controller.signal);
        if(!active)return;
        if(n.ReadAt||n.DismissedAt||n.ResolvedAt){close(tag);return;}
        const prefs=current.current.value;
        if(!prefs.enabled||!noticeBaseline||noticeBaseline.has(n.ID)||focused()&&(n.AssistantID&&reading(n.AssistantID)||location.hash===noticeRoute(n)))return;
        if(!await claims.claim(account,tag,1)||!active)return;
        const group=`notice-group:${n.Kind}:${n.AssistantID||n.Source.ID}`;
        if(!await claims.claim(account,group,Math.floor(Date.now()/5000))||canceled())return;
        const latest=await api.get<SystemNotice>(`/api/notifications/${id}`,controller.signal);
        if(canceled()||latest.ReadAt||latest.DismissedAt||latest.ResolvedAt||!current.current.value.enabled)return;
        const previous=noticeGroups.get(group);if(previous&&previous!==tag)close(previous);noticeGroups.set(group,tag);
        const content=current.current.noticeText.text(n), route=noticeRoute(n);
        const title=prefs.preview?content.title:'Molr',body=prefs.preview?content.description:current.current.noticeText.copy.unknown;
        if(prefs.sound&&sound.state()==='ready'&&await claims.claimSound(account)&&!canceled())sound.play();
        if(canceled())return;
        if(!focused()&&prefs.system&&device.capability()==='granted'){
          try{await device.show({tag,title,body,route,silent:true});if(canceled())await device.close(tag);return;}catch{/* Fall back to an in-app reminder. */}
        }
        toasts.set(tag,current.current.toast.toast({title,description:body,duration:8000,action:{label:current.current.noticeText.copy.open,onClick:()=>{void api.post('/api/notifications/update',{IDs:[n.ID],Action:'read'}).catch(()=>{});openRoute(route);}}}));
      }catch{/* Never display inaccessible or stale data. */}
    }
    async function event(event: RuntimeEvent) {
      if(event.Topic==='notification.changed'&&event.Object?.ID){await deliverNotice(event.Object.ID);return;}
      if (event.Topic === 'conversation.read') { const id = event.ConversationID ?? event.Object?.ID; if (id) close(id); return; }
      const prefs = current.current.value, conversation = event.ConversationID, id = event.Object?.ID;
      if (!baseline.current || !prefs.enabled || event.Topic !== 'message.created' || !conversation || !id) return;
      try {
        const [page, assistants] = await Promise.all([
          api.get<Page<Message>>(`/api/conversations/${encodeURIComponent(conversation)}/messages?message_id=${encodeURIComponent(id)}`, controller.signal),
          api.all<Assistant>('/api/assistants', controller.signal),
        ]);
        const message = page.Items?.find(item => item.ID === id), assistant = assistants.find(item => item.ConversationID === conversation);
        if (!active || !message || message.Sender !== 'assistant' || message.Content.Card?.Kind === 'permission_request' || message.Sequence <= (baseline.current?.get(conversation) ?? 0) || !assistant || assistant.State !== 'active') return;
        let batch = pending.get(conversation);
        if (!batch) {
          suppressed.delete(conversation);
          channel?.postMessage({ type: 'probe', assistant: assistant.ID, conversation });
          batch = { assistant, messages: [], timer: setTimeout(() => void deliver(conversation), focused() ? 350 : 650) };
          pending.set(conversation, batch);
        }
        batch.messages.push(message);
      } catch { /* A removed or inaccessible message is never notified. */ }
    }
    const unsubscribe = subscribeEvents(event);
    return () => {
      active = false; clearTimeout(retryNotices); unsubscribe(); controller.abort(); channel?.close(); claims.close(); close(); sound.dispose(); audio.current = null; device.dispose(); adapter.current = null;
      window.removeEventListener('click', unlockSound); window.removeEventListener('keydown', unlockSound);
      window.removeEventListener('focus', sync); document.removeEventListener('visibilitychange', sync); window.removeEventListener('storage', storage);
    };
  }, [account, key, subscribeEvents, createAdapter]);
  async function requestPermission() {
    const device = adapter.current; if (!device) return;
    try { const next = await device.requestPermission(); if (adapter.current === device) setCapability(next); } catch { if (adapter.current === device) setCapability(device.capability()); }
  }
  async function test() {
    const device = adapter.current, copy = current.current.copy;
    if (!device) return false;
    const notification = { tag: 'test', title: copy.testTitle, body: copy.testBody, route: '' };
    if (current.current.value.system && device.capability() === 'granted') {
      try { await device.show(notification); return true; } catch { /* A test still works when the system channel fails. */ }
    }
    if (adapter.current === device) current.current.toast.toast({ title: notification.title, description: notification.body });
    return false;
  }
  async function testSound() {
    const sound = audio.current;
    if (!sound) return false;
    return await sound.unlock() && audio.current === sound && sound.play();
  }
  return <NotificationContext.Provider value={{ preferences: value, change, capability, soundState, testSound, requestPermission, test }}>{children}</NotificationContext.Provider>;
}
