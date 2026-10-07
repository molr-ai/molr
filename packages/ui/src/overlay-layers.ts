// Modal order is explicit: document order does not describe nested/reopened dialogs.
// Subscribers move persistent toast hosts without remounting their timed contents.
type ModalLayer = { dialog: HTMLDialogElement; boundary: HTMLElement | null; parent: HTMLDialogElement | null };
const layers: ModalLayer[] = [];
const subscribers = new Set<() => void>();

export function registerModalLayer(dialog: HTMLDialogElement, boundary: HTMLElement | null, parent: HTMLDialogElement | null = null) {
  const layer = { dialog, boundary, parent };
  const childIndex = layers.findIndex(existing => existing.parent === dialog);
  if (childIndex < 0) layers.push(layer);
  else {
    layers.splice(childIndex, 0, layer);
    // A responsive parent can become modal while a child is already open.
    // Re-establish native top-layer order as well as the toast host order.
    if (dialog.matches(':modal')) {
      for (const existing of layers.slice(childIndex + 1)) {
        let ancestor = existing.parent;
        while (ancestor && ancestor !== dialog) ancestor = layers.find(item => item.dialog === ancestor)?.parent ?? null;
        if (ancestor === dialog && existing.dialog.matches(':modal')) {
          existing.dialog.close();
          existing.dialog.showModal();
        }
      }
    }
  }
  for (const notify of subscribers) notify();
  return () => {
    const index = layers.indexOf(layer);
    if (index !== -1) layers.splice(index, 1);
    for (const notify of subscribers) notify();
  };
}
export function currentModalLayer(boundary: HTMLElement | null) {
  return [...layers].reverse().find(layer => layer.dialog.isConnected && layer.dialog.open && (!layer.boundary || layer.boundary === boundary));
}
export function subscribeModalLayers(notify: () => void) {
  subscribers.add(notify);
  return () => { subscribers.delete(notify); };
}
