# Molr Brand and UI Basics

The brand name is **Molr**, and the official website domain is `molr.ai`.

## Visual Direction

**Rational structure, warm expression.**

Adopt the temperament of a lightweight research tool: light backgrounds, dark green text, ample whitespace, moderate rounded corners, using background colors to distinguish hierarchy, and retaining soft, thin borders only on necessary separators, secondary buttons, and input controls. Lime green is used only to emphasize important nodes. The interface centers on content and clear operational hierarchy.

## Brand Colors

| Color | Value | Usage |
| --- | --- | --- |
| Forest · Dark Moss Green | `#263B31` | Primary brand color, headings, body text, primary buttons |
| Lime · Lime Green | `#D5F478` | Brand emphasis, selected visual nodes, key entry points |
| Canvas · Warm Mist White | `#F5F6EF` | Page background |
| White · Pure White | `#FFFFFF` | Content containers, input fields |
| Sage · Gray Green | `#657368` | Secondary text |
| Mist · Misty Gray Green | `#DCE1D6` | Weak borders, necessary separators |

The recommended visual proportion for light backgrounds, dark green, and lime green is approximately **80 / 15 / 5**. Use dark green text on lime backgrounds. The default border for input controls and Select components uses `#DCE1D6`, slightly darkening to `#C5CDC1` on hover. On focus, use the focus semantic color to thicken the border inward. Error states prioritize the error color.

The calculated contrast ratios for default pairings are: Dark Green / Warm White **11.02:1**, Gray Green / Warm White **4.59:1**, Dark Green / Lime **9.72:1**. Re-check when adding new pairings.

Success, warning, and error states use independent green, amber, and red semantic variables respectively, and are always paired with text. The brand accent color does not replace status information.

## Light and Dark Themes

The interface follows the system by default. Users can manually select light or dark mode on the right side of the navigation, and the selection is saved. The `data-theme` of the root element uses `light` or `dark`, and shared components adapt via semantic variables. The dark theme continues the brand temperament with a dark moss green background, warm white text, and lime accents:

| Semantic Role | Light | Dark |
| --- | --- | --- |
| Page Background | `#F5F6EF` | `#131C17` |
| Card and Input Surfaces | `#FFFFFF` | `#1C2922` |
| Body Text | `#263B31` | `#F5F6EF` |
| Auxiliary Text | `#657368` | `#A8B6AA` |
| Section Separators | `#DCE1D6` | `#35493C` |
| Control Borders | `#DCE1D6` | `#35493C` |
| Control Hover Borders | `#C5CDC1` | `#49604F` |

The calculated contrast ratio for dark theme body text against the page background is **16.01:1**, and for auxiliary text against card surfaces is **7.15:1**. Weak borders only assist in identifying controls; focus states use a distinct focus color. Status labels use their respective dark backgrounds and light text. Danger buttons still use a dark red background with white text.

Brand color cards display original color values. The lime primary visual uses dark green text; light and dark Logo samples retain their respective backgrounds. Navigation and footer use inverted white wordmarks and lime cat eyes in the dark theme. The appearance theme is independent of the Logo's day/night action periods, and animated interactions do not display text tooltips.

## Logo

**The Molr cat-inspired wordmark is finalized as a light-stroke, compact, slightly outward-flaring version.** The four letters suggest a cat's posture: the high points at both ends of **M** suggest upright ears, with a rounded transition in the center. The lower ends of the two straight lines on the sides each move outward by 1.25 design units, forming a symmetrical outward flare of approximately 2.5°. The two internal dots suggest cat eyes. `o` retains the original slightly vertical Bezier curves, scaled proportionally to 5/6, without changing to perfect circles or horizontal flattening. The end of `l` curves slightly, and `r` transitions smoothly from the vertical stroke into an upward-curving final stroke, with the tail tip aligned with the tops of M and l.

The wordmark uniformly uses rounded lines with a 7-unit radius. The top ends of M are located at (10, 12) and (52, 12), and the bottom ends at (8.75, 41) and (53.25, 41). Both eyes have a diameter of 8, with centers at (23, 36) and (39, 36), symmetrically placed inside the M. All four letters share the same baseline, and r is a single continuous stroke. The static version, the resting pose of animations, the standalone M, and the app icon share the same geometry.

On light backgrounds, use dark moss green glyphs and cat eyes `#263B31`; on dark backgrounds, use warm white glyphs `#F5F6EF`, with cat eyes in brand lime green `#D5F478`. The app icon uses a lime background, dark moss green M, and cat eyes.

The full wordmark is used for site navigation, footer, and brand displays; the standalone **cat-ear M** is used for app icons, avatars, and favicons. Do not overlay a standalone M before the primary wordmark to avoid repetition.

Resources are located in the shared UI package at [`public/brand`](../../../../packages/ui/public/brand). Both the wordmark and the standalone M provide a static version, 12 independent animations, and two combined performances, each covering light and dark color schemes:

| Resource | Usage |
| --- | --- |
| `molr-wordmark.svg` / `molr-wordmark-light.svg` | Full wordmark for light / dark backgrounds |
| `molr-mark.svg` / `molr-mark-light.svg` | Standalone M for light / dark backgrounds |
| `molr-icon.svg` | Lime-background app icon and favicon (static only) |

Animation resources add an animation suffix to the end of the above wordmark / standalone M filenames, such as `molr-wordmark-curious.svg`, `molr-mark-light-sleepy.svg`.

The default `/brand` and built-in `/brand/themes/<palette>` color schemes for `Logo` use SVGs bundled with the frontend code. After the page loads, the initial reconnection loading, restoration greeting, replay, and day/night switching do not require server requests. Images use data URLs, and interactive animations use same-origin Blob URLs from accessible SVG clocks, released upon unmounting or replay. `ConnectionLogo` retains the static image until the animation loads successfully. Custom resource directories still use the original addresses, falling back to built-in static images upon load failure.

`npm run brand:generate` updates both the downloadable SVG and `packages/ui/src/logo-assets.json`; changes to the graphics must be committed together. Do not manually modify the packaged copies.

| Animation | Suffix / `animation` | Rhythm and Action | Applicable Scenarios |
| --- | --- | --- | --- |
| Blink | `blink` | Blinks lightly once every 4 seconds, approx. 200 ms | Avatars, brand displays |
| Leisure | `idle` | 2.8-second loop; cat ears retract and stretch, tail tip bends slightly in sync | Idle interfaces |
| Curious | `curious` | 1.8 seconds, plays once then returns to position; one ear lifts slightly, head and both eyes tilt slightly | Response on hover, keyboard focus |
| Dozing | `sleepy` | 24 seconds, plays once; eyes slowly narrow into arcs, ear tips and tail tip relax, eyes remain narrowed for 20.4 seconds before opening | Long periods of inactivity |
| Greeting | `greet` | 2.4 seconds, plays once then returns to position; nods, blinks, tail tip sways twice; standalone M retains nodding and blinking | First entry, welcome |
| Loading | `loading` | The ring stroke of 'o' completes one uniform circle every 1.6 seconds; cat ears stretch gently with a 3.2-second cycle, tail tip follows slightly later, eyes and 'l' remain stable. Standalone M uses a stroke that deforms with the outline to indicate loading | Processing, waiting for results |
| Looking Around | `look` | 3.6 seconds, both eyes look to both sides, head follows gently, then looks back forward | Observing surroundings |
| Listening | `listen` | 2 seconds, one ear perks up and moves slightly, pauses briefly then returns to position | Noticing sound or change |
| Stretching | `stretch` | 3.2 seconds, contracts body then stretches, glyphs extend lightly, tail tip follows slightly later | Transitioning from rest to active |
| Nuzzling | `nuzzle` | 2.6 seconds, narrows eyes and leans lightly to one side, tail tip curls then returns to position | Affection, response |
| Ear Twitch | `earflick` | 1.6 seconds, ear tips twitch in sequence, then natural blink | Brief small movement |
| Peeking | `peek` | 2.8 seconds, head leans forward slightly, eyes widen slightly, tail tip lifts then retracts | Active observation, prelude to greeting |

All six new standalone actions play once and then return to position, and can be replayed via `replayKey`.

Combined performances offer two personalities, cycling in a fixed order with pauses of 15–30 seconds between actions; Blink and Leisure play consecutively for 3–5 rounds first, then enter the pause; Dozing plays only once, extending the narrowed-eye duration:

| Preset | `animation` / SVG Suffix | Duration | Playback Order |
| --- | --- | --- | --- |
| Quiet Curiosity | `alive` | 299.6 seconds | Greeting → Looking Around → Dozing → Listening → Curious → Leisure × 5 → Stretching → Nuzzling → Blink × 4; pauses of 20–30 seconds between segments |
| Affectionate Playfulness | `playful` | 200.4 seconds | Greeting → Peeking → Ear Twitch → Blink × 3 → Nuzzling → Looking Around → Greeting → Stretching → Curious; pauses of 15–25 seconds between segments |

`animation="daily"` rotates based on device local time: plays Quiet Curiosity from 08:00 (inclusive) to 20:00 (exclusive), and Affectionate Playfulness from 20:00 to 08:00 the next day. The page switches automatically at 08:00 and 20:00, and verifies the time period upon resuming from background; crossing midnight at night does not restart. Users can also manually select either set. The combined SVG contains the complete timeline and can loop independently after download; day/night rotation is handled by the component selecting the current file, so there is no separate `daily.svg`.

The combined Logo supports `interactive`: clicking the Logo during the waiting interval, or pressing Enter / Space after focusing, immediately advances to the next stage; waiting in the final segment jumps to the beginning of the next round. Actions in progress and consecutive rounds remain complete; repeated clicks do not skip. Animations do not display status text or hover tooltips, retaining screen reader labels; mouse, touch, and keyboard are all operable; when reduced motion is enabled, a static logo is displayed.

Playback order, duration per cycle `duration`, number of consecutive cycles `cycles`, and pause after segment `pause` are maintained by [`logo-choreography.json`](../../../../packages/ui/src/logo-choreography.json). Run the generation command after modification. `cycles` defaults to 1; only blinking and casual actions can be set to multiple cycles. Pauses occur only after all cycles of the action are completed. Each cycle connects at the static pose, with eyes moving along with the head to maintain glyph legibility at small sizes. Loading is reserved for actual processing states; use expression actions for daily performances.

All animations are encapsulated within SVG and used directly via `<img>`. Outline deformation uses native SVG path interpolation, requiring no JavaScript; interactive components use the actual clock of the same-source SVG to skip waiting. When the system's "reduce motion" setting is enabled, the original static outline is displayed. The control points of the central arc of M are calculated based on the direction of the adjacent ear lines; the starting tangent of the tail curve of r remains horizontal. Static and animated poses share `logo-contours.mjs`, ensuring that line segment connections remain tangent during keyframes and interpolation. `packages/ui/tests/logo-contours.test.mjs` checks the connection and consistency between exported and built-in resources.

Curiosity and greeting are triggered by the caller when the corresponding interaction occurs; incrementing `replayKey` of `Logo` can replay the animation. The preview page provides a "Replay" button; clicking animation options or switching graphics also plays from the beginning. The loading animation indicates indeterminate progress; pair it with readable status text in actual use.

Color schemes are customizable. The generation script includes two presets: the light preset uses deep moss green for both strokes and cat eyes, while the dark preset inverts to warm white strokes with lime green cat eyes. Override any brand color via environment variables to re-export the entire matrix; if only some parameters are passed, the rest use brand defaults:

```bash
MOLR_INK='#1b2a22' MOLR_ACCENT='#c8f25d' npm run brand:generate
# 或直接调用脚本
node packages/ui/scripts/generate-logos.mjs --ink '#1b2a22'
```

After customizing colors, re-verify contrast according to the pairing rules in the brand color section.

The wordmark consists of vector paths and does not rely on user-installed fonts. Maintain at least half the width of M as clear space around the wordmark; the full Molr wordmark is recommended to be no less than 96 px wide; the standalone icon is recommended to be no less than 24 px; the favicon uses dedicated icon resources. For smaller display spaces, prioritize the standalone M.

The visible top line for M / l / tail tip is y = 8.5, and the common baseline is y = 44.5; the wordmark canvas is 183 × 54, the standalone graphic is 64 × 64, and the app icon is 80 × 80. Finalized geometry and colors are maintained by [`generate-logos.mjs`](../../../../packages/ui/scripts/generate-logos.mjs). Run `npm run brand:generate` in the repository root to export 62 SVG and 20 PNG app icon resources.

The standalone [UI Design app](../../../../apps/ui-design/README.md) displays usage of light backgrounds, dark backgrounds, and app icons in `#identity`, supporting day/night rotation, click to skip waiting, manual preset selection, viewing individual actions one by one, as well as the full wordmark / standalone M, and downloading the SVG for the corresponding preview.

## Fonts and Shapes

- English: **DM Sans**, loaded locally by the project. Chinese: System sans-serif fonts, prioritizing PingFang and Microsoft YaHei.
- Base font sizes: Title 32 px, body 16 px, controls 14 px, auxiliary 12 px.
- Spacing: 4 px is the base, commonly used values are 4 / 8 / 12 / 16 / 24 / 32 / 48 / 64.
- Border radius: Controls 8 px, cards 16 px, tags use pill shape.
- Hierarchy: Prioritize whitespace, surface colors, and thin borders. Animations primarily use state transitions of approximately 160 ms, respecting the system's "reduce motion" setting.

## Basic Components

| Component | Convention |
| --- | --- |
| `Button` | Five variants: primary action, emphasis, secondary, text, danger; three height tiers: 32 / 40 / 48 px; supports disabled state |
| `TextField` | Always displays label, supports description, error, and disabled states |
| `Select` | Unified single-select dropdown; supports icons, descriptions, placeholders, error and disabled states, keyboard navigation, and light/dark themes |
| `Badge` | Neutral, success, warning, error; status is expressed by both text and color |
| `Switch` | Native checkbox control with readable label, supports keyboard and disabled states |
| `Card` | Uses theme surface color, thin border, 16 px border radius, no shadow by default |
| `Checkbox` / `RadioGroup` / `SegmentedControl` | Multi-select and indeterminate, native radio, compact segmented options; disabled and focus states are unified |
| `Textarea` / `SearchField` | Multi-line input and clearable search; follows input box label, border, and error semantics |
| `Tabs` / `Breadcrumb` / `Pagination` | Content categorization, current location, and pagination; retains keyboard operation and narrow screen adaptation |
| `Dialog` / `Drawer` | Modal editing and side details; drawers use edge-aligned right angles, slide in/out from the right, and restore focus after exit completes |
| `DropdownMenu` / `Popover` / `Tooltip` | Action menus, interactive details, and supplementary notes; follows unified popup visual style |
| `Alert` / Toast | In-page messages and corner notifications; provides retry, close, or undo actions; status is expressed by both text and color |
| `EmptyState` / `Skeleton` / `Progress` | Explains empty states, indicates waiting, and presents actual progress; retains static information when motion is reduced |
| `Table` | Semantic tables, controlled sorting, multi-select, loading, and empty states; scrolls within the table area on narrow screens |
| `ResourceCard` / `TaskCard` | Combines source, status, progress, and related actions into consistent content cards |
| Chat group (Chat in English version) components | Bubbles have no border and contain only text, avatars are outside the bubble and can be independently toggled, images, attachments, and cards are displayed independently; input box has 8 px border radius, adaptive height, focus shown only on the outer frame; message info is collapsed into an icon, like/dislike placed below the bubble; actions, thinking, typing, etc. use independent assistant status components |
| `Logo` | Dark, light, graphic only; static by default, supports all independent actions, `alive` / `playful` combinations, and `daily` day/night rotation; increment `replayKey` to replay, `interactive` enables click-to-continue when waiting is active |

Tabs and grouped options retain only two visual styles: **underline minimal version** and **segmented control with background**. Underlines are used for content categorization and in-page navigation, with transparent background and a 2 px underline for the selected item. Segmented controls are used for modes, presets, and compact filters, uniformly using neutral background, surface color for selected items, and light shadow; outer border radius 8 px, inner border radius 4 px, padding and item spacing both 4 px. Both styles share 14 px font size, disabled and focus states; ensure each item is at least 44 px high on touchscreens and narrow screens. Component categorization, platform switching, Logo animation selection, and schedule view all reuse shared styles.

Implementation is located in [`packages/ui/src/components`](../../../../packages/ui/src/components), reusable variables are in [`packages/ui/src/tokens.css`](../../../../packages/ui/src/tokens.css). The application uses components and styles via `@molr/ui` and `@molr/ui/styles.css`. Light theme focus states use deep green outlines, dark theme uses lime outlines, and all interactive elements retain visible keyboard focus.

For complete component interfaces and usage, see the [Component Library Documentation](../../../../packages/ui/COMPONENTS.md). `UIProvider` uniformly provides built-in Chinese and English labels; inputs, navigation, feedback, popups, and data examples on the display page share these components.

Dropdown triggers follow the input box's 8 px border radius and control border, with heights of 40 / 44 px. Popups use theme surface color, 12 px border radius, thin borders, and light shadows; options express the current value with a light background and checkmark, distinguish active items with outlines, and reduce emphasis for disabled items. Navigation theme switching and form selection share the same component; menus automatically adjust position on narrow screens and container edges.

This version establishes the foundation for brand and general interface. The homepage displays style references; input validation, color value copying, switches, and resource downloads are functional, but product business flows are not yet defined.

### Assistant Message Input and Message Container

The message input container follows `--radius-control` (8 px), while message content and attached cards follow `--radius-card` (16 px). They serve different purposes and may use different border radii, but similar surfaces should not define other values independently. The textarea within the combined input does not draw its own border radius, border, or focus outline; the outer container provides focus hints by changing border color via `:focus-within`.

The input area starts with one line, grows with content, and scrolls internally after reaching 180 px. Visible labels are replaced by placeholders, while screen reader names are retained. The toolbar left side contains attachments, right side contains model, voice, and send. Enter sends, Shift+Enter creates a new line; Enter does not send during IME composition.

Messages support body text, images, attachments, static cards, and feedback. Both assistant and user messages use borderless 16 px rounded text bubbles, distinguishing roles via surface color and alignment; images, attachments, and cards are displayed independently of the bubble; task actions are accessed via a drawer entry in the chat header, with no attached action buttons within messages. Performance info is collapsed into an info icon popup below the message, not spread out in the message flow by default; unread counts use red-background white-text badges. Performance info is based on actual obtainable measurements; when no real model is connected, display "—", do not use simulated values to fake real performance.

## Document Scope

This file maintains visual variables, Logo, and component specifications; specific product layouts are handled by the application. Appearance preferences for the official website / UI Design can be saved in the browser; theme settings for the real WebUI are saved by the Runtime account. Extension slots supported by the input, such as models and voice, do not imply that every product page has enabled them; actual chat capabilities are subject to the [Product Guide](../../user/guides/assistants-and-chat.md).
