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
languages, plus user/public and administrator presentation cases. It checks translated navigation, segmented choices, row
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

The user layout fixture mounts the real AppLayout, sidebar, header and Select.
It checks independent narrow-screen inset expectations (16/24 CSS px) and desktop
centered content (94% of the main viewport, capped at 1600 CSS px) at 320,
390, 767, 768, 1023, 1024, 1440, 1920 and 3072px, expanded/collapsed sidebars,
natural user-page scrolling in a 600px window, fixed user record workspaces, ordinary pages and empty/partial
card groups. Actual DataTable cases cover content-sized left-aligned columns,
single-line model/group labels, wrapping actions and complete first-column hover
and empty-row borders. Dense-record fallbacks retain multiple field columns on
desktop, one on phones, and full-width selection and action rows.
Administrator fixed-scroll ownership remains covered.
The administrator fixture checks the original 12/16/24px shell insets, expanded
and collapsed sidebars, independent toolbar and pagination surfaces, pinned
columns during horizontal scrolling, full mobile record fields and actions,
and neutral colors in body-portalled controls across both themes and languages.
Centered distribution tables keep their scrollport within the parent card while
preserving wide columns and synchronized horizontal header scrolling.
Real user usage records also exercise mouse-wheel scrolling in the native table
and responsive cards, fixed pagination, and server requests for the selected page.
It also checks body-portalled user colors and preserved admin default insets.
Additional 390px and 1440px overlay projects exercise the public/user palette in
both themes, including configured accent and transparency. Synthetic stores and
intercepted API responses keep the layout fixture independent of any instance.

The motion projects use a separately production-built entrypoint through the
application's PostCSS/Tailwind pipeline. Real dialog, drawer, announcement,
header, subscription, version, select, checkbox, refresh and calendar controls
exercise the approved 200/140ms dialog and 150/100ms menu durations. Frame samples
check root timing, continuous panel movement, DOM retention through leave,
position-preserving calendar fades and reduced motion. This catches production
purging of Vue's dynamically applied transition classes; JSDOM class assertions
cannot establish that contract.

The server and browser processes stop when Playwright exits. The outer
validation launcher owns removal of its one-shot container and temporary output
directory (`SUB2API_LAYOUT_OUTPUT_DIR`, default `/tmp/sub2api-layout-results`).
No screenshots, traces or videos are retained.
