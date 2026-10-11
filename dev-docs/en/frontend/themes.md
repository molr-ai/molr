# Theme and Page Resolution

The theme determines the page implementation, and users select a theme identity. For architectural division of labor, see [Frontend Architecture](architecture.md); for colors and same-screen checks, see [Design System](design-system.md).

## Product Interaction

In User Settings → Theme, there is only one "Theme" selector, with the following built-in options:

- Molr Default Theme
- Molr Test Theme

"Theme, Display Mode, Reduce Motion" are placed in the same group, while "Theme Color Scheme" is the last group. Display Mode still provides Follow System, Light, and Dark. Theme Color Schemes provide Forest, Sea Salt Blue, Iris Purple, Warm Sand, Rose, and Plain Ink; Plain Ink overrides interface base colors and SVG brand assets, while status, categories, and code syntax continue to use shared semantic colors. The settings list only allows interaction with the controls themselves; clicking on titles or blank row areas no longer expands selectors or toggles switches; keyboard operations and accessibility names are preserved.

Users select a complete theme. **Which pages are replaced is determined by the theme manifest; users cannot combine pages individually.** The Test Theme only replaces the chat page, changing the assistant list to a top selector; other pages automatically use the Default Theme. Chat business logic, messages, attachments, and drafts continue to be shared.

No standalone theme management page or per-page selector is provided. External themes are imported, authorized, and enabled by administrators via the "Plugins" page, and then appear in the theme selector; plugin versions and installations are uniformly managed by Plugin Management. The personal theme page address is `/#/account/theme`; `/#/settings/*` is used for administrator settings.

## Theme Selection and Page Resolution

Account settings save only one theme identity and do not save user-defined page mappings. Runtime's `Settings.UITheme` uses `id@version`, which the frontend resolves as:

```json
{
  "schema": 2,
  "selected": { "id": "molr.test", "version": "1.0.0" }
}
```

The theme itself declares page implementations:

```ts
const testTheme = {
  runtime: 'builtin',
  manifest: {
    id: 'molr.test', name: 'Molr 测试主题', version: '1.0.0', apiVersion: 1,
    pages: { assistants: 'test-chat' },
    permissions: ['chat.read', 'chat.write', 'files.read', 'files.write'],
  },
};
```

During resolution, the system first checks the `pages[page]` of the selected theme; if undeclared, it uses the corresponding page from `molr.official@1.0.0`. Built-in themes and authorized plugin themes follow the same rule; some themes do not need to implement a full set of pages.

The directory retains two built-in themes and adds enabled, authorized plugin themes returned by Runtime. Legacy fields in the browser such as `installed` and `routes` do not participate in resolution; unknown theme identities fall back to the Default Theme. Data from the previous per-page configuration prototype will not be re-enabled.

Theme selection, theme color scheme, and display preferences are uniformly saved in Runtime's account settings and synchronized across devices. `Settings.PluginsVisible` defaults to `false`; when disabled, plugin manifests are not read, the theme directory contains only built-in themes, and saved plugin theme identities are retained but temporarily fall back to the Default Theme. Plugin routes redirect to General Settings; navigation, plugin tool directories, plugin targets in login links, user group plugin/connection options, and usage connection details are synchronously hidden; editing user groups still retains references to unshown resources. This setting does not change server-side plugin enablement, authorization, or execution. On new device login, saved selections are read; already open devices refresh using the settings snapshot, updating when the page becomes visible again or during periodic refreshes. Theme selection does not rely on localStorage; if saving fails, the original selection is retained, and if a theme is unavailable, the Default Theme is used. Writes use settings version validation, merging only theme fields; if old clients omit `UITheme`, saved values are retained. Chat drafts after theme switching continue to be preserved.

## Verification

`apps/webui/tests/ui-packages.spec.ts` verifies single theme selection, partial page replacement owned by themes, fallback for undeclared pages, rejection of legacy browser configuration access, cross-browser synchronization, shared drafts and real sending, and server-side save failures, as well as the original chat loading flow. Theme and login regression covers light/dark modes, theme color schemes, account isolation, and one-time login.
