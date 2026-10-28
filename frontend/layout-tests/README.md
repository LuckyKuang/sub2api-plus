# Shared component layout regressions

Run **inside the repository validation container** from the repository root:

```sh
pnpm --dir frontend run test:layout
```

This starts an independent local Vite entrypoint and uses the pinned Chromium
already present in `deploy/Dockerfile.validation`. It needs no application
backend, accounts, credentials, or external services. It imports production
Vue components, English/Chinese messages, and `src/style.css`; the fixture CSS
only creates clipping/scrolling containers. It does not change production
routes, build inputs, or the separate seven-provider business suite.

The matrix covers 375×812, 768×900, 1000×600 and 1440×900 in both themes and
languages (144 cases). It checks translated navigation, segmented choices, row
actions, form controls and button labels at 0%, 20% and 100% transparency,
configured accent extremes at 0%, 50% and 100% transparency (including immediate
theme switching), date ranges and their nested calendars,
plain/searchable selection menus, checkbox dropdowns inside clipping cards, and dialogs with long titles, footer labels
and body content.
Assertions use actual viewport bounds, nested-container bounds and
`elementFromPoint` at multiple positions to detect clipping and covering layers;
they also exercise the final option, final content and apply/save actions.
Keyboard cases reach the start date and apply button by Tab, then verify that
successive Escape presses close only the calendar, range and parent dialog in
that order and restore focus to the corresponding trigger.
Checkbox menus also open from the keyboard, accept multiple choices without
closing, and return Tab/Shift+Tab to the surrounding form's normal order.
JSDOM component tests cannot establish these geometry requirements.

The server and browser processes stop when Playwright exits. The outer
validation launcher owns removal of its one-shot container and temporary output
directory (`SUB2API_LAYOUT_OUTPUT_DIR`, default `/tmp/sub2api-layout-results`).
No screenshots, traces or videos are retained.
