// Native tab stops used by both dialogs and their portaled notification controls.
const selector = 'button, a[href], input, select, textarea, audio[controls], video[controls], [contenteditable], summary, [tabindex]';
export function getTabStops(root: HTMLElement): HTMLElement[] {
  const candidates = [...root.querySelectorAll<HTMLElement>(selector)].filter(element => {
    if (element.matches(':disabled') || element.closest('[inert], [hidden]') || !element.getClientRects().length || ['hidden', 'collapse'].includes(getComputedStyle(element).visibility)) return false;
    // Closed details can retain layout boxes; only their first summary is tabbable.
    for (let details = element.parentElement?.closest('details:not([open])'); details; details = details.parentElement?.closest('details:not([open])')) {
      const summary = [...details.children].find(child => child.tagName === 'SUMMARY');
      if (!summary?.contains(element)) return false;
    }
    if (element.hasAttribute('tabindex')) return element.tabIndex >= 0;
    if (element.matches('summary')) return element.parentElement?.tagName === 'DETAILS' && element.parentElement.querySelector('summary') === element;
    if (element.hasAttribute('contenteditable')) return element.isContentEditable && !element.parentElement?.isContentEditable;
    return element.tabIndex >= 0 || element.matches('audio[controls], video[controls]');
  });
  return candidates.filter(element => {
    if (!(element instanceof HTMLInputElement) || element.type !== 'radio' || !element.name) return true;
    const group = candidates.filter((candidate): candidate is HTMLInputElement => candidate instanceof HTMLInputElement && candidate.type === 'radio' && candidate.name === element.name && candidate.form === element.form);
    return element === (group.find(candidate => candidate.checked) ?? group[0]);
  }).sort((a, b) => (a.tabIndex > 0 ? a.tabIndex : Infinity) - (b.tabIndex > 0 ? b.tabIndex : Infinity));
}
