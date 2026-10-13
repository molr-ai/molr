// Docked panels leave the chat usable; modal dialogs and drawers obscure it.
export function isChatObscured() {
  return !!document.querySelector('dialog[open]:not([data-docked="true"])');
}

export function onChatRevealed(callback: () => void) {
  let obscured = isChatObscured();
  const observer = new MutationObserver(() => {
    const next = isChatObscured();
    if (obscured && !next) callback();
    obscured = next;
  });
  // Closing, unmounting and switching a drawer to docked mode can all reveal
  // the chat without changing the message sequence or the window's focus.
  observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['open', 'data-docked'] });
  return () => observer.disconnect();
}
