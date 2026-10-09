# Retty settings design QA

final result: passed

## Visual evidence

- Source: `docs/settings-design/reference.png`, copied from the user-selected settings reference `/var/folders/0d/hh4p_gx94p78zvmvtgq7884w0000gn/T/codex-clipboard-3lnoDm.png`.
- Implementation: `docs/settings-design/settings-general-dark.png`, rendered by the actual MyGo settings view; the same view was opened and checked in the native Retty Dev window using Computer Use.
- Source pixels: 3024 × 1898. Implementation: 3024 × 1896, viewport 1512 × 948 points at 2× density. Implementation was normalized to 3024 × 1898 for comparison; the two-pixel height difference is capture framing.
- State: General settings, dark appearance. The reference is tty7; implementation content is Retty's existing preferences, using their defaults. Native inspection also covered the user's enabled compact mode and saved hidden-header/host preferences.
- Full-view paired comparison: `docs/settings-design/comparison.png`.
- Focused paired sidebar comparison: `docs/settings-design/sidebar-comparison.png`.
- Light appearance: `docs/settings-design/settings-general-light.png`.
- Minimum window: `docs/settings-design/settings-small-dark.png`, 560 × 340 points at 2× density.

This is an update to an existing native app, not a browser prototype. The OS draws the traffic lights; headless screenshots omit those native controls. Native window inspection confirmed they remain available.

## Comparison history

1. Initial native inspection found [P1] sidebar icons and labels centered inside full-width buttons. The user also requested left alignment. Fixed with start justification; the revised native screenshot and paired sidebar image confirm a shared left edge and retained full-row selection background.
2. Initial inspection found [P2] compact mode disabled entire rows, reducing explanatory-text contrast. Changed this to disable switches and reset controls while keeping their labels and explanations readable. The revised native General view confirms both the disabled state and legible text.
3. Post-fix comparison and responsive captures show no remaining actionable P0/P1/P2 findings. The same tests exercise cross-section search, modified filtering, resetting a value, small-window access, and focus restoration.

## Required fidelity surfaces

- Fonts and typography: system UI sans serif with native Chinese fallback, 19-point page title, 14-point section headings, 13-point setting labels, 11.5-point descriptions. Hierarchy, wrapping, and weight are consistent with the reference. Reviewed readable native screenshots and the focused sidebar crop.
- Spacing and layout: full-window flat surface, 216-point sidebar, centered 640-point content column, consistent setting/control alignment, section rules, independently scrolling content. At 560 points the sidebar reduces to 152 points; persistent navigation, close button, filter, and completion button remain within the window. Long sections scroll.
- Colors and tokens: charcoal main surface, slightly lighter sidebar, muted descriptions, blue selected navigation and enabled switches. The light variant uses equivalent neutral surfaces and blue selection. Disabled controls retain their disabled appearance while their context remains readable.
- Image quality and assets: no decorative raster assets in the reference. Existing Lucide SVG icons are reused at native scale; no fabricated icon art or placeholder imagery.
- Copy and content: Chinese navigation and descriptive text. Only supported Retty preferences are included: host visibility, pane headers, compact mode, appearance, and font size. Reference-only language, SSH, tray, startup and integration controls were intentionally not introduced. Search, modified-state labels, reset actions, and empty states are functional.

## Interaction validation

- Native Retty Dev: ⌘, opening, sidebar navigation, General and Appearance states, current theme selection, search-field initial focus, disabled compact-mode-dependent controls, and left-aligned menu rendering.
- Automated actual-view tests: global ⌘F search, filtering across sections, font-stepper keyboard changes and persisted values, modified-only filtering, per-setting restore and persistence, all four sidebar sections, header/host/compact controls, Escape and completion-button closing, search/terminal focus restoration, session continuity, and minimum-window controls.
- Full test suite: 55 passed across 7 packages. `go vet ./...` and `git diff --check` passed. Native app and DMG built successfully.

## Follow-up polish

- [P3] The reference has more settings and therefore fills more of its content area. Retty deliberately shows its smaller, supported preference set.
- [P3] Minor sidebar row-height and icon-color differences remain; the menu is left-aligned as requested. The OS controls are verified in the native window rather than synthesized in headless captures.

## Implementation checklist

- [x] Full-window settings layout and left-aligned navigation.
- [x] Existing preferences apply immediately and persist.
- [x] Search, modified filter, and restore controls work.
- [x] Light/dark and minimum-window layouts reviewed.
- [x] Modal keyboard isolation and focus restoration verified.
- [x] Paired source/implementation comparison reviewed after fixes.
