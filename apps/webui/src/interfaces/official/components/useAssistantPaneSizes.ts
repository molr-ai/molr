import { useLayoutEffect, useRef, useState } from 'react';
import { useBreakpoint } from '@molr/ui';
import { useSession } from '../../../state';
import type { ChatPanelSelection } from './ChatSidePanel';

type PanelKind = ChatPanelSelection['kind'];
type Sizes = Partial<Record<'list' | PanelKind, number>>;
export const LIST_MIN = 224, CHAT_MIN = 360;
export function panelMinimum(kind: PanelKind | undefined) { return kind === 'tasks' || kind === 'shell_tasks' ? 320 : 360; }
const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(value, max));
function readSizes(key: string): Sizes {
  try {
    const value = JSON.parse(localStorage.getItem(key) ?? '{}');
    return Object.fromEntries(['list', 'info', 'permissions', 'tasks', 'shell_tasks', 'browser', 'files', 'terminal'].flatMap(kind =>
      typeof value?.[kind] === 'number' && Number.isFinite(value[kind]) && value[kind] > 0 ? [[kind, value[kind]]] : []));
  } catch { return {}; }
}

export function useAssistantPaneSizes(element: HTMLElement | null, kind: PanelKind | undefined, docked: boolean) {
  const { principal } = useSession();
  const compact = useBreakpoint('compact');
  const key = `molr-assistant-panes:${principal!.OwnerID}`;
  const [sizes, setSizes] = useState(() => readSizes(key));
  const current = useRef(sizes);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    if (!element) return;
    const update = () => setWidth(element.getBoundingClientRect().width);
    update();
    const observer = new ResizeObserver(update); observer.observe(element);
    return () => observer.disconnect();
  }, [element]);
  const panelMin = panelMinimum(kind), gaps = docked ? 16 : 8;
  const list = clamp(sizes.list ?? (compact ? 224 : 256), LIST_MIN, width - CHAT_MIN - gaps - (docked ? panelMin : 0));
  const panelMax = Math.max(panelMin, width - list - CHAT_MIN - gaps);
  const panel = clamp((kind && sizes[kind]) ?? (kind === 'tasks' || kind === 'shell_tasks' ? 480 : 600), panelMin, panelMax);
  const listMax = Math.max(LIST_MIN, width - CHAT_MIN - gaps - (docked ? panel : 0));
  function resize(target: 'list' | PanelKind, value: number) {
    const next = { ...current.current, [target]: Math.round(value) };
    current.current = next; setSizes(next);
  }
  function save() {
    try { localStorage.setItem(key, JSON.stringify(current.current)); } catch { /* Keep resizing usable when browser storage is unavailable. */ }
  }
  return { list, listMax, panel, panelMin, panelMax, resize, save };
}
