# Use a browser

Assistants can open websites, read pages, fill forms, click controls and capture screenshots. The **Browser** button in the chat header shows the same browser the assistant is using.

## Start and watch

An administrator must configure the [browser programs](../sandbox/linux.md#managed-browser-tasks), and the assistant needs local compute and the required network grants. For example:

> [!PROMPT]
> Open this website, compare the specifications of two products, and summarize the result. Wait for me to take control if login is needed.

You can also select an authorized local compute resource in the browser panel and choose **Start browser**. Each assistant has at most one active browser task; another start reuses it. The browser supports multiple tabs and counts toward existing execution and compute limits.

Closing the panel only stops viewing. Close the browser through the browser panel or running tasks. Normal closure retains website login state and local data for the next launch; websites determine whether a login remains valid. Different assistants do not share browser data.

## Take and return control

Choose **Take control** and wait for an already accepted operation to settle. You can then click, scroll, drag, navigate and switch tabs. After clicking a field in the page, type directly or use the input area beneath the view for IME text, pasting and special keys.

During takeover the assistant can still read snapshots, screenshots, tabs and the current URL. Clicking, filling, navigating, closing and other operations return an explicit takeover message. Control applies to all tabs. A website you log into here is available to the assistant after handoff within the same grants.

Choose **Return control** when finished. The assistant receives a notification and must read a fresh snapshot before continuing. Queued operations and stale control generations are not replayed. Concurrent takeover requests from multiple windows reject stale versions.

Disconnecting or closing the panel does not return control automatically. Reopen it to take control again or explicitly hand it back. The page's own scripts and networking remain subject to the original sandbox permissions; takeover does not expand them.

## Scope and failures

The current implementation supports Chromium in the local Linux sandbox. Programs use a shared read-only installation; website data persists per assistant. Live viewing does not automatically record video, and human keystrokes are not written to the operation log. Screenshots are saved only when explicitly requested.

Missing programs or libraries, insufficient resources and expired grants appear as unavailable or ended tasks. Normal closure lets Chromium save its data before reclaiming the execution. Forced termination, crashes and revocation may lose changes that have not reached disk. Runtime restart marks tasks interrupted and never replays their operations.
