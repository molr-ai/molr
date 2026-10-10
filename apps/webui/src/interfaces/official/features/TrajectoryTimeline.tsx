import { useMemo, useRef, useState } from 'react';
import { Button, RangeSlider } from '@molr/ui';
import { Minus, Plus, RotateCcw, SlidersHorizontal } from 'lucide-react';
import { useCopy } from '../../../state';
import { durationLabel } from './trajectory';
import { axisPositions, axisRangeIndices } from './trajectory-axis';
import type { AxisMode, AxisRange, AxisItem } from './trajectory-axis';

type Props = { items: AxisItem[]; mode: AxisMode; onModeChange: (mode: AxisMode) => void; range: AxisRange | null; onRange: (range: AxisRange | null) => void; onSelect: (item: AxisItem) => void; selected?: string; timezone?: string };
export function TrajectoryTimeline({ items, mode, onModeChange, range, onRange, onSelect, selected, timezone }: Props) {
  const { t, locale } = useCopy();
  const [zoom, setZoom] = useState<AxisRange | null>(null), [brush, setBrush] = useState<[number, number] | null>(null);
  const rangePanel = useRef<HTMLDetailsElement>(null);
  const pointer = useRef<{ x: number; at: number; id: number } | null>(null), dragged = useRef(false);
  const eventMode = mode === 'event';
  const positions = useMemo(() => axisPositions(items, mode), [items, mode]);
  const min = positions.length ? positions.reduce((value, row) => Math.min(value, row.start), Infinity) : 0;
  const max = positions.reduce((value, row) => Math.max(value, row.end), min + 1);
  const selectedIndices = range?.mode === 'event' ? axisRangeIndices(items, range) : null;
  const selectedBounds: [number, number] | null = range?.mode === 'time' ? range.bounds : selectedIndices ? [selectedIndices[0], selectedIndices[1] + 1] : null;
  const zoomIndices = zoom?.mode === 'event' ? axisRangeIndices(items, zoom) : null;
  const zoomBounds: [number, number] | null = zoom?.mode === 'time' ? zoom.bounds : zoomIndices ? [zoomIndices[0], zoomIndices[1] + 1] : null;
  const start = zoomBounds ? Math.max(min, Math.min(max - 1, zoomBounds[0])) : min;
  const end = zoomBounds ? Math.max(start + 1, Math.min(max, zoomBounds[1])) : max;
  const span = Math.max(1, end - start), selection = brush ?? selectedBounds;
  const percent = (value: number) => Math.max(0, Math.min(100, (value - start) / span * 100));
  const atPointer = (x: number, panel: HTMLElement) => { const rect = panel.getBoundingClientRect(); return start + Math.max(0, Math.min(1, (x - rect.left) / rect.width)) * span; };
  const includeDate = max - min > 86400000;
  const timeFormat = useMemo(() => new Intl.DateTimeFormat(locale, { ...(includeDate ? { month: 'short' as const, day: 'numeric' as const } : {}), hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3, hour12: false, timeZone: timezone }), [locale, timezone, includeDate]);
  const formatTime = (at: number) => timeFormat.format(at);
  const eventSelection = (first: number, last: number): AxisRange | null => items.length ? { mode: 'event', first: items[Math.max(0, Math.min(items.length - 1, Math.round(first)))].id, last: items[Math.max(0, Math.min(items.length - 1, Math.round(last)))].id } : null;
  const selectBounds = (bounds: [number, number]) => onRange(eventMode ? eventSelection(bounds[0], bounds[1]) : { mode: 'time', bounds });
  const brushRange = (bounds: [number, number]) => selectBounds(eventMode ? [Math.floor(bounds[0]), Math.max(Math.floor(bounds[0]), Math.ceil(bounds[1]) - 1)] : bounds);
  const scale = (factor: number) => {
    const center = selectedBounds ? (selectedBounds[0] + selectedBounds[1]) / 2 : (start + end) / 2;
    const width = Math.min(max - min, Math.max(1, eventMode ? Math.round(span * factor) : span * factor));
    const left = Math.max(min, Math.min(max - width, eventMode ? Math.floor(center - width / 2) : center - width / 2));
    setZoom(width >= max - min ? null : eventMode ? eventSelection(left, left + width - 1) : { mode: 'time', bounds: [left, left + width] });
  };
  const triggers = t.workbench.triggers as Record<string, string>;
  const rangeLabel = eventMode ? t.workbench.eventRange : t.workbench.timeRange;
  return <section className="trajectory-overview" aria-label={t.workbench.timeOverview} data-axis-mode={mode}>
    <div className="trajectory-overview-heading">
      <div className="trajectory-axis-modes" role="group" aria-label={t.workbench.axisMode}>{(['time', 'event'] as const).map(value => <button key={value} type="button" aria-pressed={mode === value} onClick={() => { if (mode === value) return; setZoom(null); setBrush(null); pointer.current = null; dragged.current = false; if (rangePanel.current) rangePanel.current.open = false; onModeChange(value); }}>{value === 'time' ? t.workbench.timeAxis : t.workbench.eventAxis}</button>)}</div>
      {range && <span className="trajectory-range-badge">{eventMode ? t.workbench.selectedEvents : t.workbench.selectedRange}</span>}<div className="trajectory-zoom">
      <details ref={rangePanel} className="trajectory-range-controls" onKeyDown={event => { if (event.key === 'Escape') { event.currentTarget.open = false; event.currentTarget.querySelector('summary')?.focus(); event.stopPropagation(); } }}><summary aria-label={rangeLabel} title={rangeLabel}><SlidersHorizontal size={13} /></summary><div className="trajectory-range-panel">
        <RangeSlider label={rangeLabel} size="sm" hideLabel disabled={!items.length} min={eventMode ? 0 : min} max={eventMode ? Math.max(0, items.length - 1) : max} step={eventMode ? 1 : Math.max(1, Math.round((max - min) / 1000))}
          value={eventMode ? selectedIndices ?? [0, Math.max(0, items.length - 1)] : range?.mode === 'time' ? range.bounds : [min, max]} onValueChange={selectBounds}
          lowerLabel={eventMode ? t.workbench.eventRangeStart : t.workbench.rangeStart} upperLabel={eventMode ? t.workbench.eventRangeEnd : t.workbench.rangeEnd}
          formatValue={value => !items.length ? '—' : eventMode ? t.workbench.eventNumber(value + 1) : formatTime(value)} description={eventMode ? t.workbench.eventRangeHelp : t.workbench.timeHelp} />
      </div></details>
      <Button size="sm" variant="ghost" aria-label={t.workbench.zoomIn} title={t.workbench.zoomIn} disabled={!items.length} onClick={() => scale(.5)}><Plus size={14} /></Button>
      <Button size="sm" variant="ghost" aria-label={t.workbench.zoomOut} title={t.workbench.zoomOut} disabled={!items.length} onClick={() => scale(2)}><Minus size={14} /></Button>
      <Button size="sm" variant="ghost" aria-label={t.workbench.resetRange} title={t.workbench.resetRange} onClick={() => { setZoom(null); onRange(null); }}><RotateCcw size={13} /></Button>
    </div></div>
    <div className="trajectory-lanes"><div className="trajectory-lane-labels" aria-hidden="true"><span>{t.workbench.triggerLane}</span><span>{t.workbench.models}</span><span>{t.workbench.toolsShort}</span></div>
    <div className="trajectory-time-track" aria-label={eventMode ? t.workbench.eventRangeHelp : t.workbench.timeHelp}
      onPointerDown={event => { if (event.button !== 0 || !event.isPrimary || !items.length) return; dragged.current = false; pointer.current = { x: event.clientX, at: atPointer(event.clientX, event.currentTarget), id: event.pointerId }; }}
      onPointerMove={event => { const down = pointer.current; if (!down || down.id !== event.pointerId) return; if (Math.abs(event.clientX - down.x) > 5) { dragged.current = true; event.currentTarget.setPointerCapture(event.pointerId); const at = atPointer(event.clientX, event.currentTarget); setBrush([Math.min(down.at, at), Math.max(down.at, at)]); } }}
      onPointerUp={event => { const down = pointer.current; if (down?.id !== event.pointerId) return; if (dragged.current) { const at = atPointer(event.clientX, event.currentTarget); brushRange([Math.min(down.at, at), Math.max(down.at, at)]); } pointer.current = null; setBrush(null); if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }}
      onPointerCancel={() => { pointer.current = null; setBrush(null); }} onPointerLeave={() => { if (!dragged.current) pointer.current = null; }}>
      {positions.map((row, index) => {
        if (eventMode ? row.start >= end || row.end <= start : row.start > end || row.end < start) return null;
        const item = row.item;
        const name = item.kind === 'trigger' ? triggers[item.run.Trigger] ?? item.run.Trigger : item.kind === 'model' ? item.record?.name === 'context.compaction' ? t.workbench.checkpointCauses.context_compaction : t.workbench.modelCall : item.record?.name || t.workbench.toolCall;
        const title = `${eventMode ? `${t.workbench.eventNumber(index + 1)} · ` : ''}${name} · ${formatTime(item.start)}${item.kind === 'trigger' ? '' : ` · ${durationLabel(item.record?.duration)}`}`;
        const width = percent(row.end) - percent(row.start);
        return <button key={item.id} type="button" className={`trajectory-time-bar trajectory-time-bar--${item.kind}`} data-state={item.state ?? item.record?.state ?? item.run.State} data-trigger={item.kind === 'trigger' ? item.run.Trigger : undefined} data-axis-index={index} data-axis-id={item.id} aria-pressed={selected === item.id}
          style={{ left: `${percent(row.start) + (eventMode ? width * .1 : 0)}%`, width: eventMode ? `${width * .8}%` : `max(4px, ${Math.max(0, width)}%)` }}
          title={title} aria-label={title} onClick={event => { if (!event.detail || !dragged.current) onSelect(item); }} />;
      })}
      {selection && <div className="trajectory-time-selection" style={{ left: `${percent(selection[0])}%`, width: `${Math.max(0, percent(selection[1]) - percent(selection[0]))}%` }} />}
    </div></div>
  </section>;
}
