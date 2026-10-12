# Use a browser

Assistants can open websites, read pages, fill forms, click controls and capture screenshots. The **Browser** button in the chat header shows the same browser the assistant is using.

## Start and watch

Grant the assistant local compute and the required network access. On first use, an administrator can choose **Install and start** in the browser panel to download and verify the compatible programs. Installation continues after the panel closes; grants are checked again before starting the browser. Administrators can also configure [browser programs](../sandbox/linux.md#managed-browser-tasks) in advance. For example:

> [!PROMPT]
> Open this website, compare the specifications of two products, and summarize the result. Wait for me to take control if login is needed.

You can also select an authorized local compute resource in the browser panel and choose **Start browser**. Each assistant has at most one active browser task; another start reuses it. The browser supports multiple tabs and runs as a sandbox task. Its compute resource determines CPU, memory, process and runtime limits under existing grants. The panel selects the first available resource by default; you can choose another. The browser counts toward existing execution and compute limits.

Closing the panel only stops viewing. Close the browser through the browser panel or running tasks. Normal closure retains website login state and local data for the next launch; websites determine whether a login remains valid. Different assistants do not share browser data.

The browser icon badge shows the open tab count even while the panel is closed. Tabs, new tab, full-page view and close share one row; takeover controls sit beside the address bar. Tabs first shrink, then left/right arrows reveal overflow. Large screens get a wider sidebar. **Full-page view** fills the Molr page while keeping tabs, navigation and control actions visible. Expansion and contraction animate between the current panel bounds and page bounds. Choose **Exit full-page view** or press Esc to return; it does not enter the device’s native fullscreen mode.

## Take and return control

Choose **Take control** and wait for an already accepted operation to settle. You can then click, scroll, drag, navigate and switch tabs. Click a field in the page to type or paste directly. Only the system IME candidate window appears near the clicked position, and committed composition text is sent once. The keyboard button opens an optional input area for mobile or other input methods. Use the + button to create a tab and the close button on each tab to close that specific tab.

During takeover the assistant can still read snapshots, screenshots, tabs and the current URL. Clicking, filling, navigating, closing and other operations return an explicit takeover message. Control applies to all tabs. A website you log into here is available to the assistant after handoff within the same grants.

Choose **Return control** when finished. The assistant receives a notification and must read a fresh snapshot before continuing. Queued operations and stale control generations are not replayed. Concurrent takeover requests from multiple windows reject stale versions. A dismissible toast asks you to take control again and also disappears automatically.

Disconnecting or closing the panel does not return control automatically. Reopening the panel in the same page restores that window’s control without another takeover. Refreshing the page or switching to another window requires an explicit takeover. The page's own scripts and networking remain subject to the original sandbox permissions; takeover does not expand them.

## Scope and failures

The current implementation supports Chromium in the local Linux sandbox. Programs use a shared read-only installation; website data persists per assistant. Live viewing does not automatically record video, and human keystrokes are not written to the operation log. Screenshots are saved only when explicitly requested.

Missing components show an installation action and storage requirements. Missing system libraries or an unavailable maintenance runner show administrator guidance. The browser panel and settings show progress and allow retrying failed installation. Runtime restart does not replay pending browser starts. Missing libraries, insufficient resources and expired grants appear as unavailable or ended tasks. Normal closure lets Chromium save its data before reclaiming the execution. Forced termination, crashes and revocation may lose changes that have not reached disk. Runtime restart marks tasks interrupted and never replays their operations.
