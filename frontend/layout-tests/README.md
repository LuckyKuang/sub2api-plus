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
Wide selection triggers also exercise the 480px menu ceiling with actual
geometry and selection of the last option.
Assertions use actual viewport bounds, nested-container bounds and
`elementFromPoint` at multiple positions to detect clipping and covering layers;
they also exercise the final option, final content and apply/save actions.
Keyboard cases reach the start date and apply button by Tab, then verify that
successive Escape presses close only the calendar, range and parent dialog in
that order and restore focus to the corresponding trigger.
Checkbox menus also open from the keyboard, accept multiple choices without
closing, and return Tab/Shift+Tab to the surrounding form's normal order.
The real API Key page checks equal prominence of both creation actions and
complete selected group names at phone, tablet and desktop widths.
Both creation actions stay visible when the query fields are folded, including
after expanding and folding them again. Long names
wrap inside the selection field, and an open checkbox menu follows its growing
height without covering the selected text.
Aggregate creation and editing also preserve the ordinary Key's platform badge
colors in both the checkbox menu and selected content, including saved choices.
Their API fixtures return the sidebar's independent image-mode capabilities
and the complete saved key detail, matching the production response contracts.
JSDOM component tests cannot establish these geometry requirements.

The user layout fixture mounts the real AppLayout, sidebar, header and Select.
It checks independent narrow-screen inset expectations (12/16 CSS px) and desktop
centered content (94% of the main viewport, capped at 1600 CSS px) at 320,
390, 767, 768, 1023, 1024, 1440, 1920 and 3072px, expanded/collapsed sidebars,
natural user-page scrolling in a 600px window, fixed user record workspaces, ordinary pages and empty/partial
card groups. Actual DataTable cases cover content-sized left-aligned columns,
single-line model/group labels, wrapping actions and complete first-column hover
and empty-row borders. Dense-record fallbacks retain multiple field columns on
desktop, one on phones, and full-width selection and action rows.
The embedded Model Plaza checks that two complete cards fit below the header
at 390×844, that copying a model ID and opening its full-screen details use
distinct click targets, and that the 1440px catalog has three cards per row.
Administrator fixed-scroll ownership remains covered. The unified image-task
case mounts the real mode cards and ordinary history at phone widths in both
themes/languages, checks their viewport bounds and confirms that disabled new
submissions preserve history and explain the reason.
The administrator fixture checks the original 12/16/24px shell insets, expanded
and collapsed sidebars, independent toolbar and pagination surfaces, pinned
columns during horizontal scrolling, full mobile record fields and actions,
and neutral colors in body-portalled controls across both themes and languages.
Real risk-control, prompt-audit and proxy views also check the accepted header
order, frameless embedded tabs, actions following the final query field,
right alignment, uniform control heights and wrapping without page overflow.
Centered distribution tables keep their scrollport within the parent card while
preserving wide columns and synchronized horizontal header scrolling.
Real user usage records also exercise mouse-wheel scrolling in the native table
and responsive cards, fixed pagination, and server requests for the selected page.
It also checks body-portalled user colors and preserved admin default insets.
Additional 390px and 1440px overlay projects exercise the public/user palette in
both themes, including configured accent and transparency. Synthetic stores and
intercepted API responses keep the layout fixture independent of any instance.
Cold production-view imports have a separate mount wait before record-count,
geometry and interaction assertions use their normal five-second limit.
Mobile subscription and payment-result cases at 320px and 390px check long
unbroken names and order references inside their cards, including renewal and
navigation action hit targets and real click destinations.

The real administrator Ops Dashboard runs against intercepted ops APIs at
428×745. It checks that the phone overview's health hover region ends inside
its card and before the concurrency card, that the latency, error-distribution
and error-trend charts each draw inside a 200 CSS px plot region whose canvas
and document heights stay stable for twelve seconds (first with latency-only
data, then after a refresh adds error data), and that the last chart card is
reachable after reload, landscape rotation and return to portrait. Wide and
fullscreen 1440px workspaces keep the three-up chart grid and error-detail
actions. The plot height is the verified design value; total page height and
hover geometry are read from the rendered elements rather than hard-coded.

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

The actual Settings page also checks its save dock at desktop and 375px widths
in both themes and English/Chinese: the final visible field and save button
remain in view and can receive pointer input, the dock stays within the viewport,
and a save reaches the settings API.

The administrator quality-inspection case mounts the production page with isolated API fixtures, checks numbered pagination and one row-action menu, confirms billable runs only after the shared confirmation dialog, and exercises the plan dialog and expanded Select in both themes/languages at phone and desktop widths.
