import { useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { ButtonHTMLAttributes, KeyboardEvent, ReactNode } from 'react';
import { useUILocale } from '../locale';
import { getOverlayViewport, useModalScope } from '../modal-scope';
import { triggerElement, useComposedRefs } from './floating';
import type { FloatingTrigger } from './floating';

export type SelectOption<Value extends string = string> = {
  value: Value;
  label: string;
  description?: string;
  icon?: ReactNode;
  disabled?: boolean;
  group?: { value: string; label: string };
};

type SelectBaseProps<Value extends string> = {
  label: string;
  options: readonly SelectOption<Value>[];
  placeholder?: string;
  description?: string;
  error?: string;
  disabled?: boolean;
  hideLabel?: boolean;
  /** Keep the accessible name without forwarding visible-label clicks to the trigger. */
  labelInteractive?: boolean;
  size?: 'sm' | 'md';
  searchable?: boolean;
  searchPlaceholder?: string;
  align?: 'start' | 'end';
  id?: string;
  name?: string;
  className?: string;
  trigger?: FloatingTrigger;
};
export type SelectProps<Value extends string = string> = SelectBaseProps<Value> & (
  | { multiple?: false; value: Value | ''; onValueChange: (value: Value) => void }
  | { multiple: true; value: readonly Value[]; onValueChange: (value: Value[]) => void }
);

type Placement = { left: number; top: number; width: number; maxHeight: number; side: 'top' | 'bottom' };

export function Select<Value extends string>(props: SelectProps<Value>) {
  const { label, options: suppliedOptions, placeholder, description, error, trigger,
    disabled = false, hideLabel = false, labelInteractive = true, size = 'md', searchable = false, searchPlaceholder, align = 'start', id, name, className = '',
  } = props;
  const options = useMemo(() => {
    const groups = new Map<string | undefined, SelectOption<Value>[]>();
    for (const option of suppliedOptions) {
      const key = option.group?.value;
      const group = groups.get(key);
      if (group) group.push(option); else groups.set(key, [option]);
    }
    return [...groups.values()].flat();
  }, [suppliedOptions]);
  const { messages } = useUILocale();
  const boundary = useModalScope()?.container;
  const generatedId = useId();
  const triggerId = id ?? generatedId;
  const labelId = `${triggerId}-label`;
  const listboxId = `${triggerId}-listbox`;
  const popupId = `${triggerId}-popup`;
  const hintId = `${triggerId}-hint`;
  const triggerRef = useRef<HTMLButtonElement>(null);
  const composedTriggerRef = useComposedRefs(triggerRef, trigger?.props.ref);
  const popupRef = useRef<HTMLDivElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const searchRef = useRef({ text: '', time: 0 });
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [placement, setPlacement] = useState<Placement | null>(null);
  const enabledIndices = useMemo(() => options.flatMap((option, index) => option.disabled ? [] : [index]), [options]);
  const visibleIndices = useMemo(() => {
    const terms = searchable ? query.trim().toLowerCase().split(/\s+/).filter(Boolean) : [];
    return options.flatMap((option, index) => {
      const text = `${option.label} ${option.value} ${option.description ?? ''} ${option.group?.label ?? ''}`.toLowerCase();
      return terms.every(term => text.includes(term)) ? [index] : [];
    });
  }, [options, query, searchable]);
  const navigableIndices = useMemo(() => visibleIndices.filter(index => !options[index].disabled), [visibleIndices, options]);
  const values: readonly Value[] = props.multiple ? props.value : [props.value as Value];
  const selectedIndex = options.findIndex(option => values.includes(option.value));
  const selected = options[selectedIndex];
  const unavailable = disabled || enabledIndices.length === 0;
  const hint = error || description;

  function show(index = selectedIndex) {
    if (unavailable) return;
    setActiveIndex(enabledIndices.includes(index) ? index : enabledIndices[0]);
    setQuery('');
    searchRef.current = { text: '', time: 0 };
    setOpen(true);
  }

  function choose(index: number, restoreFocus = true) {
    const option = options[index];
    if (!option || option.disabled || unavailable) return;
    if (props.multiple) props.onValueChange(values.includes(option.value) ? values.filter(value => value !== option.value) : [...values, option.value]);
    else {
      if (option.value !== props.value) props.onValueChange(option.value);
      setOpen(false);
    }
    if (restoreFocus) (searchable && props.multiple ? filterRef.current : triggerRef.current)?.focus({ preventScroll: true });
  }

  useLayoutEffect(() => {
    if (open && !navigableIndices.includes(activeIndex)) {
      setActiveIndex(navigableIndices.includes(selectedIndex) ? selectedIndex : navigableIndices[0] ?? -1);
    }
  }, [open, activeIndex, navigableIndices, selectedIndex]);

  useLayoutEffect(() => {
    const popup = popupRef.current;
    const trigger = triggerRef.current;
    if (!popup || !trigger) return;
    if (!open || unavailable) {
      if (popup.matches(':popover-open')) popup.hidePopover();
      if (open) setOpen(false);
      return;
    }
    if (!popup.matches(':popover-open')) popup.showPopover();

    function position() {
      if (!popup || !trigger) return;
      const rect = trigger.getBoundingClientRect();
      const { left: viewportLeft, top: viewportTop, width: viewportWidth, height: viewportHeight } = getOverlayViewport(boundary);
      if (viewportWidth <= 16 || viewportHeight <= 16 || rect.bottom < viewportTop || rect.top > viewportTop + viewportHeight) {
        setOpen(false);
        return;
      }
      const margin = 8;
      const gap = 6;
      const width = Math.min(Math.max(rect.width, 224), viewportWidth - margin * 2);
      const left = Math.max(viewportLeft + margin, Math.min(align === 'end' ? rect.right - width : rect.left, viewportLeft + viewportWidth - width - margin));
      // Measure at the final width, since option descriptions may wrap.
      popup.style.width = `${width}px`;
      const contentHeight = searchable ? Array.from(popup.children).reduce((height, child) => height + (child === resultsRef.current ? child.scrollHeight : child.getBoundingClientRect().height), 12) : popup.scrollHeight;
      const desiredHeight = Math.min(contentHeight + 2, 320);
      const below = viewportTop + viewportHeight - rect.bottom - gap - margin;
      const above = rect.top - viewportTop - gap - margin;
      const side = below < desiredHeight && above > below ? 'top' : 'bottom';
      const maxHeight = Math.max(0, Math.min(320, side === 'top' ? above : below));
      const height = Math.min(desiredHeight, maxHeight);
      const top = Math.max(viewportTop + margin, side === 'top' ? rect.top - gap - height : rect.bottom + gap);
      const next: Placement = { left, top, width, maxHeight, side };
      setPlacement(previous => previous && Object.keys(next).every(key => previous[key as keyof Placement] === next[key as keyof Placement]) ? previous : next);
    }

    function handleScroll(event: Event) {
      if (event.target instanceof Node && popup?.contains(event.target)) return;
      position();
    }

    position();
    const observer = new ResizeObserver(position);
    observer.observe(trigger);
    observer.observe(popup);
    if (boundary) observer.observe(boundary);
    window.addEventListener('resize', position);
    window.addEventListener('scroll', handleScroll, true);
    window.visualViewport?.addEventListener('resize', position);
    window.visualViewport?.addEventListener('scroll', position);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', position);
      window.removeEventListener('scroll', handleScroll, true);
      window.visualViewport?.removeEventListener('resize', position);
      window.visualViewport?.removeEventListener('scroll', position);
    };
  }, [open, unavailable, align, options, boundary, searchable, size]);

  useLayoutEffect(() => {
    if (open && searchable && !unavailable) filterRef.current?.focus({ preventScroll: true });
  }, [open, searchable, unavailable]);

  useLayoutEffect(() => {
    if (!open) return;
    const popup = searchable ? resultsRef.current : popupRef.current;
    const option = document.getElementById(`${listboxId}-${activeIndex}`);
    if (!popup || !option) return;
    // Scroll only the options; keyboard navigation must not move the page.
    if (option.offsetTop < popup.scrollTop) popup.scrollTop = option.offsetTop;
    else if (option.offsetTop + option.offsetHeight > popup.scrollTop + popup.clientHeight) {
      popup.scrollTop = option.offsetTop + option.offsetHeight - popup.clientHeight;
    }
  }, [open, activeIndex, listboxId, placement?.maxHeight, searchable, visibleIndices]);

  function handleKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (unavailable || event.ctrlKey || event.metaKey || event.nativeEvent.isComposing) return;
    const { key } = event;
    if (key === 'Tab') {
      if (open) { if (props.multiple) setOpen(false); else choose(activeIndex, false); }
      return;
    }
    if (key === 'Escape') {
      if (open) {
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        if (searchable) triggerRef.current?.focus({ preventScroll: true });
      }
      return;
    }
    if (event.altKey && key === 'ArrowUp') {
      if (open) { event.preventDefault(); if (props.multiple) setOpen(false); else choose(activeIndex); }
      return;
    }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End', 'PageDown', 'PageUp', 'Enter', ' '].includes(key)) {
      event.preventDefault();
      if (key === 'Enter' || key === ' ') {
        if (open) choose(activeIndex); else show();
      } else if (key === 'Home' || key === 'End') {
        const indices = open ? navigableIndices : enabledIndices;
        const index = key === 'Home' ? indices[0] : indices[indices.length - 1];
        if (open) setActiveIndex(index); else show(index);
      } else if (!open) {
        show(key === 'ArrowUp' ? enabledIndices[0] : selectedIndex);
      } else if (!event.altKey) {
        const delta = key === 'ArrowDown' ? 1 : key === 'ArrowUp' ? -1 : key === 'PageDown' ? 10 : -10;
        const position = Math.max(0, Math.min(navigableIndices.length - 1, navigableIndices.indexOf(activeIndex) + delta));
        setActiveIndex(navigableIndices[position] ?? -1);
      }
      return;
    }
    if (key.length !== 1 || event.altKey) return;
    event.preventDefault();
    const now = performance.now();
    const text = now - searchRef.current.time < 700 ? searchRef.current.text + key.toLowerCase() : key.toLowerCase();
    const query = [...text].every(character => character === text[0]) ? text[0] : text;
    const current = open ? activeIndex : selectedIndex;
    const start = Math.max(0, enabledIndices.indexOf(current) + (query.length === 1 ? 1 : 0));
    const ordered = [...enabledIndices.slice(start), ...enabledIndices.slice(0, start)];
    const match = ordered.find(index => [options[index].label, options[index].value].some(text => text.toLowerCase().startsWith(query)));
    if (!open) show(match ?? selectedIndex);
    else if (match !== undefined) setActiveIndex(match);
    searchRef.current = { text, time: now };
  }

  function handleFilterKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing || event.ctrlKey || event.metaKey) return;
    if (event.key === 'Tab') {
      // Return to the trigger before the browser advances to the next control.
      setOpen(false);
      triggerRef.current?.focus({ preventScroll: true });
    } else if (['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Enter', 'Escape'].includes(event.key)) {
      handleKeyDown(event);
    }
  }

  const controlProps: ButtonHTMLAttributes<HTMLButtonElement> = {
    id: triggerId, type: 'button', role: 'combobox', disabled: unavailable,
    'aria-labelledby': labelId, 'aria-describedby': hint ? hintId : undefined,
    'aria-invalid': error ? true : undefined, 'aria-haspopup': searchable ? 'dialog' : 'listbox',
    'aria-expanded': open, 'aria-controls': searchable ? popupId : listboxId,
    'aria-activedescendant': !searchable && open && activeIndex >= 0 ? `${listboxId}-${activeIndex}` : undefined,
    onClick: event => { trigger?.props.onClick?.(event); if (!event.defaultPrevented) open ? setOpen(false) : show(); },
    onKeyDown: event => { trigger?.props.onKeyDown?.(event); if (!event.defaultPrevented) handleKeyDown(event); },
    onBlur: event => { trigger?.props.onBlur?.(event); if (!popupRef.current?.contains(event.relatedTarget)) setOpen(false); },
  };
  function renderOption(index: number) {
    const option = options[index];
    return <div
      key={`option-${option.value}`}
      id={`${listboxId}-${index}`}
      role="option"
      aria-selected={values.includes(option.value)}
      aria-disabled={option.disabled || undefined}
      className="select__option"
      data-active={activeIndex === index || undefined}
      onPointerMove={event => {
        if (event.pointerType === 'mouse' && !option.disabled) setActiveIndex(index);
      }}
      onClick={() => choose(index)}
    >
      {option.icon && <span className="select__icon" aria-hidden="true">{option.icon}</span>}
      <span className="select__option-text"><span className="select__option-label">{option.label}</span>{option.description && <span className="select__option-description">{option.description}</span>}</span>
      <span className="select__check" aria-hidden="true">{values.includes(option.value) && <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="m5 12 4 4L19 6" /></svg>}</span>
    </div>;
  }
  const optionElements: ReactNode[] = [];
  for (let position = 0; position < visibleIndices.length;) {
    const index = visibleIndices[position];
    const group = options[index].group;
    if (!group) {
      optionElements.push(renderOption(index)); position++;
      continue;
    }
    const children: ReactNode[] = [];
    while (position < visibleIndices.length && options[visibleIndices[position]].group?.value === group.value) {
      children.push(renderOption(visibleIndices[position++]));
    }
    const groupLabelId = `${listboxId}-group-${index}`;
    optionElements.push(<div key={`group-${group.value}`} role="group" aria-labelledby={groupLabelId} className="select__group">
      <div id={groupLabelId} className="select__group-label">{group.label}</div>{children}
    </div>);
  }
  return (
    <div className={`select select--${size}${trigger ? ' select--custom-trigger' : ''} ${className}`}>
      <label id={labelId} htmlFor={labelInteractive ? triggerId : undefined} className={`select__label${hideLabel ? ' select__label--hidden' : ''}`}>{label}</label>
      {trigger ? triggerElement(trigger, composedTriggerRef, controlProps) : <button
        ref={triggerRef}
        {...controlProps}
        className="select__trigger"
        data-placeholder={!selected || undefined}
      >
        {selected?.icon && <span className="select__icon" aria-hidden="true">{selected.icon}</span>}
        <span className="select__value">{props.multiple ? options.filter(option => values.includes(option.value)).map(option => option.label).join(', ') || placeholder || messages.select : selected?.label ?? placeholder ?? messages.select}</span>
        <svg className="select__chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
      </button>}
      {hint && <p id={hintId} className={`field-hint${error ? ' field-hint--error' : ''}`}>{hint}</p>}
      {name && (props.multiple ? values.map(value => <input key={value} type="hidden" name={name} value={value} disabled={unavailable} />) : <input type="hidden" name={name} value={props.value} disabled={unavailable} />)}
      <div
        ref={popupRef}
        id={searchable ? popupId : listboxId}
        popover="auto"
        role={searchable ? 'dialog' : 'listbox'}
        aria-multiselectable={!searchable && props.multiple || undefined}
        aria-labelledby={labelId}
        className={`select__popup select__popup--${size}${searchable ? ' select__popup--searchable' : ''}`}
        data-side={placement?.side}
        style={placement ? { left: placement.left, top: placement.top, width: placement.width, maxHeight: placement.maxHeight } : undefined}
        onPointerDown={event => { if (!(event.target instanceof HTMLInputElement)) event.preventDefault(); }}
        onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget) && event.relatedTarget !== triggerRef.current) setOpen(false); }}
        onToggle={event => {
          if (event.newState === 'closed' && !event.currentTarget.matches(':popover-open')) setOpen(false);
        }}
      >
        {searchable ? <>
          <div className="select__search">
            <input ref={filterRef} className="input" type="search" role="combobox" aria-label={messages.searchOptions(label)} placeholder={searchPlaceholder ?? messages.searchOptionsPlaceholder}
              autoComplete="off" spellCheck={false} value={query} aria-autocomplete="list" aria-expanded={open} aria-controls={listboxId}
              aria-activedescendant={open && activeIndex >= 0 ? `${listboxId}-${activeIndex}` : undefined}
              onChange={event => { setQuery(event.target.value); setActiveIndex(-1); }} onKeyDown={handleFilterKeyDown} />
          </div>
          <div ref={resultsRef} className="select__results" id={listboxId} role="listbox" aria-labelledby={labelId} aria-multiselectable={props.multiple || undefined}>{optionElements}</div>
          {visibleIndices.length === 0 && <p className="select__empty" role="status">{messages.noOptions}</p>}
        </> : optionElements}
      </div>
    </div>
  );
}
