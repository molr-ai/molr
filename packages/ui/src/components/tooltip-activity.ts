// Tooltip instances share document activity listeners. Each instance remembers
// the input version at suspension, so fresh input can release every instance
// without visiting every closed tooltip on every key or pointer press.
const subscribers = new Set<() => void>();
let inputVersion = 0;

export function tooltipInputVersion() { return inputVersion; }
export function tooltipDocumentActive() {
  return document.visibilityState === 'visible' && document.hasFocus();
}
export function suspendTooltips() { subscribers.forEach(listener => listener()); }
function visibility() { if (document.visibilityState !== 'visible') suspendTooltips(); }
function interact() { if (tooltipDocumentActive()) inputVersion++; }

export function subscribeTooltipActivity(listener: () => void) {
  if (!subscribers.size) {
    window.addEventListener('blur', suspendTooltips);
    document.addEventListener('visibilitychange', visibility);
    document.addEventListener('keydown', interact, true);
    document.addEventListener('pointerdown', interact, true);
  }
  subscribers.add(listener);
  if (!tooltipDocumentActive()) listener();
  return () => {
    subscribers.delete(listener);
    if (!subscribers.size) {
      window.removeEventListener('blur', suspendTooltips);
      document.removeEventListener('visibilitychange', visibility);
      document.removeEventListener('keydown', interact, true);
      document.removeEventListener('pointerdown', interact, true);
    }
  };
}
