# Official Usage Details Design QA

- source visual truth path: `C:/Users/Even/AppData/Local/Temp/codex-clipboard-dea53e31-babc-4771-a89f-0bee9d5c11a8.png`
- implementation screenshot path: `C:/Users/Even/.codex/visualizations/2026/09/30/01a0f0c4-e290-7b81-85ce-b2db36fd8d8c/official-usage-desktop-final.png`
- comparison image path: `C:/Users/Even/.codex/visualizations/2026/09/30/01a0f0c4-e290-7b81-85ce-b2db36fd8d8c/official-usage-comparison.png`
- error-state screenshot path: `C:/Users/Even/.codex/visualizations/2026/09/30/01a0f0c4-e290-7b81-85ce-b2db36fd8d8c/official-usage-error.png`
- mobile screenshot path: `C:/Users/Even/.codex/visualizations/2026/09/30/01a0f0c4-e290-7b81-85ce-b2db36fd8d8c/official-usage-mobile-fixed.png`
- viewport: desktop `1200 x 1200`; mobile `390 x 844`
- pixels and density: source `1200 x 1200`; desktop implementation `1200 x 1200`; mobile implementation `390 x 844`; CSS viewport and image pixels were 1:1 at device scale factor 1, so no density normalization was required
- state: Chinese locale, light theme, OpenAI OAuth parent account, successful official settlement response; separate unauthorized upstream state

## Full-view comparison evidence

- The implementation preserves the source hierarchy: account context, official/local tab switch, range control, refresh, official totals, cycle estimate, daily cost bars, client split, and model/speed split.
- The layout intentionally follows the existing Sub2API account statistics modal instead of copying the reference application's outer navigation and six-card grid.
- Settled daily bars and official cost use green; upstream authorization/service errors use a red semantic panel; unsettled data remains amber.

## Focused region comparison evidence

- Header and controls: verified account-name truncation, range selection, refresh interaction, and official-tab lazy load.
- Summary and chart: verified cost/token/turn cards, cycle progress, daily bar scaling, token and turn labels, and green settlement state.
- Responsive layout: verified the modal at `390 x 844`; `document.body.scrollWidth === document.body.clientWidth` and `.modal-body.scrollWidth === .modal-body.clientWidth`.
- Error state: verified the unauthorized state renders a red alert with a sanitized user-facing message.
- Console: no warning or error entries during final desktop interaction.

## Findings

- No actionable P0/P1/P2 differences remain.
- P3 accepted difference: the implementation is denser than the reference and uses the product's existing modal title, cards, tokens, and spacing. This is intentional adaptation, not visual drift.

## Comparison history

1. Initial mobile capture found a P2 horizontal overflow in the account header and narrow daily rows.
2. Fixed the header with `min-w-0`, truncation, and shrink constraints; changed daily rows to responsive `minmax(0, 1fr)` columns.
3. Post-fix mobile capture confirmed no page or modal horizontal overflow.

## Primary interactions tested

- Open official settlement tab.
- Switch range from 30 days to 7 days and confirm selected styling.
- Trigger manual refresh.
- Render successful, unauthorized, desktop, and mobile states.

final result: passed

## Blue Theme Revision (2026-10-13)

Latest user direction supersedes the earlier monochrome/emerald appearance:
blue primary controls, 12px cards, soft shadows, and static sidebar stars.
Usage tab/table negative margins were removed. Browser measurements confirm
18px desktop and 14px mobile gaps, with no document overflow at 1440/375px.
Light/dark usage screenshots and mobile profile editing were inspected;
Shift+Tab stays inside the profile dialog. Screenshot evidence:
`docs/screenshots/global-console/usage-blue-stars-desktop.png`.

Validation: 149 of 150 focused tests initially passed. The search/dialog
keyboard conflict was fixed; all 17 dialog/search regression tests then passed.
ESLint, type checking, locale checks, and production build passed. The final
spacing-only adjustment was checked in the browser after that build.
The preview uses synthetic data. Full production account flows and every admin
screen were not rechecked in this visual revision. Existing large bundle warnings
remain; static stars introduce no animation loop or external request.

Self-review (10-point scale): function/code/security 9.5; UI/UX/art 9.3;
performance 9.3. Deliverable within the checked scope; remaining improvements
are broader admin visual coverage, user aesthetic acceptance, and bundle sizing.

## Typography Revision (2026-10-13)

Added self-hosted Noto Sans SC Variable after Manrope for consistent Chinese
glyphs. Fontsource supplies unicode-range subsets and font-display: swap.
Console body/menu/table text is 14px, section labels 12px, desktop page titles
20px/600; mobile titles remain 18px to fit the compact header. Secondary text
contrast was strengthened in both themes. Numeric tabular figures are preserved.

Evidence: browser reports both font families loaded; 1440px and 375px usage
views have no document overflow; light/dark screenshots inspected. Screenshot:
`docs/screenshots/global-console/typography-desktop.png`.
Sample secondary-text contrast: 5.74:1 on white and 8.47:1 on dark surfaces.
Production build (including vue-tsc and locale checks), scoped ESLint and diff
whitespace checks passed. No behavioral code changed or new unit tests added.

Self-review: function/code/security 9.5, UI/UX/art 9.4, performance 9.3.
Remaining checks below 9.5: wider page/device visual coverage and cold-cache
font transfer profiling on a throttled production connection. Existing chunk
size warnings remain. Font files are served locally without third-party requests.

## Dashboard Refinement (2026-10-13)

Console Latin, Chinese and numeric text now use Noto Sans SC consistently;
homepage Manrope assets remain available for its existing canvas typography.
Platform numeric labels inherit the normal font with tabular figures.
Recent usage and quick actions use divided rows without nested colored cards.
Model distribution uses a smaller, thinner ring and matching table swatches.
Token trends use restrained colors, shared font, fewer grid/tick marks and
hover-only points; datasets and click/navigation behavior are preserved.

Verified desktop light/dark and 375px mobile rendering with synthetic data;
no document overflow. Screenshot: `docs/screenshots/global-console/dashboard-refined.png`.
11 chart/stat tests, scoped ESLint, final production build/typecheck/locales,
and diff whitespace check passed. An intermediate build during font dependency
restoration failed; the final build after restoration succeeded.
Self-review: function/code/security 9.5, UI/UX/art 9.4, performance 9.3.
Remaining improvements: wider device coverage, aesthetic acceptance and existing
large bundles. Mobile model table retains horizontal scrolling for all columns.

## Finance Layout Revision (2026-10-13)

Core metrics retain framed surfaces; secondary metrics form an unframed strip.
Platform breakdown and quick actions use separators and whitespace. Metric
colors are neutral except semantic status; numeric weight is reduced. Desktop
charts use 40/60 columns, with model details below the ring and more trend space.
Sidebar stars and user-selected blue accent remain intact.

Desktop light/dark and 375px mobile views inspected using synthetic data;
no document overflow. Screenshot: `docs/screenshots/global-console/dashboard-finance.png`.
11 chart/stat tests, scoped ESLint, production build/typecheck/locale checks
passed. No business logic changed.
Self-review: function/code/security 9.5; UI/UX/art 9.4; performance 9.3.
Remaining improvements below 9.5: broader device/production data visual review,
user aesthetic acceptance, and existing bundle-size warnings.

## Detail Polish (2026-10-13)

Replaced the model ring with labeled token-share meters, preserving request,
token and actual cost metadata and an expandable standard/actual cost table.
Recent usage now aligns model, token and cost columns; standard cost appears
on a secondary line. Mobile uses an explicit two-column grid. Trend height is
fixed to avoid canvas growth when details change the neighboring panel height.

Validation: 8 stats tests plus 2 new share/zero/empty/detail tests passed;
scoped ESLint, typecheck and production build passed. Final CSS-only chart
height/mobile adjustments visually checked at 1440px and 375px, light/dark.
Screenshot: `docs/screenshots/global-console/dashboard-polished.png`.
No document overflow observed. Synthetic preview data used.
Self-review: function/code/security 9.5; UI/UX/art 9.4; performance 9.3.
Remaining: real-world long-data/device coverage, user aesthetic acceptance,
and existing production bundle warnings. No new runtime dependency.

## Theme-Aware Sidebar and Balance (2026-10-13)

Balance now has a dedicated black surface with explicit light text. Shared
sidebar becomes white in light mode; dark mode retains the star background.
Brand, navigation, active/hover, search, keyboard hint and footer colors adapt
for both user and administrator menus. No route-specific business logic changed.
28 sidebar/search/stats tests, scoped ESLint and diff checks passed. User
dashboard light/dark behavior was visually verified; screenshot:
`docs/screenshots/global-console/balance-theme-light.png`.
Admin users shell rendered with white sidebar, but its list raised an undefined
length error with the incomplete synthetic API fixture. Full admin interactions
are not verified; use a complete admin fixture or real development backend next.
Self-review within styling scope: code/security/performance 9.5, UI/art 9.4.
Admin functional/UX verification is blocked by the fixture limitation above;
do not interpret this as acceptance of all management workflows.

---

# Orbitix-inspired Home and Integrated Console QA — 2026-10-11

## Scope and acceptance

Adapt the reference at https://orbitix.site/home to the existing Sub2API brand. Support readable light and dark themes across the default home, authentication layout and console. Use a continuous edge-aligned sidebar/header; remove decorative console motion while preserving loading feedback. Preserve custom-home/compact-home precedence, registration and feature flags, authentication and routing.

Acceptance requires a successful production build/typecheck, lint, relevant regression tests, real desktop/mobile screenshots, theme/navigation/form checks, and evidence that the console does not load the homepage particle bundle. Live API latency is outside this frontend-only verification.

## Evidence

Artifacts are under `output/orbitix-design-review/` (local, ignored output):

- Reference: `01-home-desktop.jpg`, `06-login-desktop.jpg`, `07-home-mobile.jpg`, `reference-overview.jpg`.
- Implementation comparison: `implementation-overview.jpg`; home: `home-{light,dark}-desktop.jpg`, `home-{light,dark}-mobile.jpg`, `home-{light,dark}-full.jpg`.
- Authentication: `login-{light,dark}-desktop.jpg`, `login-dark-mobile.jpg`.
- Console: `dashboard-{light,dark}-desktop.jpg`, `keys-{light,dark}-desktop.jpg`, `keys-light-mobile.jpg`, `keys-light-tablet.jpg`.
- Motion/locale: `home-reduced-motion-tablet.jpg`, `home-english-mobile.jpg`.
- Verification logs: `build.log`, `lint.log`, `final-focused-tests.log`, `tests.log`, `baseline-tests.log`.

Desktop CSS viewport: 1440 × 900; phone: 390 × 844 and 320 × 740; tablet: 768 × 1024. Browser screenshots sometimes exclude scrollbar pixels; the overview normalizes each image proportionally to a 720 × 450 tile. No pixel-exact clone claim is made: light mode and a non-floating console are intentional user-requested adaptations.

## Results

- Production build (including vue-tsc and locale completeness): passed. ESLint and `git diff --check`: passed.
- Final relevant regression run: 27 files, 221 tests passed. Includes home override precedence, register/login/admin destinations, reduced motion, animation pause/resume, configured API endpoint, auth flows, sidebar, dialogs, tables and locales.
- Full suite: 439 files passed; 3 files / 10 tests failed; 9 skipped. The same 10 failures reproduce against an isolated, unmodified HEAD frontend using the same installed dependencies: AccountUsageCell has 8 missing-helper failures, ModelWhitelistSelector has 1 expectation failure, IQTestModal has 1 model-selection expectation failure. These are not repaired by this UI change.
- Both themes rendered with no page-level horizontal overflow at checked breakpoints. Mobile navigation opens at full width even after desktop collapse; hidden navigation is inert; Escape closes it and returns focus to the menu toggle. Collapsed links retain accessible names.
- Local fixture login reaches the real dashboard route. Key table, horizontal table scrolling, create-dialog opening/cancel, theme change, home anchors, FAQ, copy feedback, and English language switch work. No production account or data was used.
- Actual console geometry: sidebar at x=0/y=0, width 256px or 72px; header at y=0, height 64px. Shell border radius 0; shadow none; sidebar transition 0s. Modal backdrop blur and modal transitions removed.
- A fresh production login/dashboard session requested no `vendor-orbit`, `OrbitScene`, `HomeView` or Manrope assets. The two dashboard canvases are charts. Particle code is confined to the async homepage component; its vendor chunk is approximately 174kB gzip.
- Browser Event Timing samples on this machine: collapse 24ms, expand 24ms, theme toggle 40ms (input delay about 0.2ms; processing 1.4ms, 0.8ms, 19.6ms). Three samples, no throttling, local fixtures: these are not field INP measurements or proof of improved network latency.
- Homepage animation stops when paused, offscreen, or when `prefers-reduced-motion: reduce` is emulated; reduced-motion scrolling is immediate. Scene is removed after navigation. Temporary browser emulation and timing observers are restored/removed after checks.
- Production preview uses nonce-based script CSP. The early theme script carries the backend's existing nonce placeholder; no unsafe script allowance was added. Browser console contains no errors in the final production login/dashboard/keys/home checks.
- Calculated solid-color contrast: light primary button 6.05:1; dark primary button 10.71:1; home muted body text 5.66:1 light / 8.46:1 dark. Keyboard focus is visible. This is not a complete screen-reader audit of every existing product page.

## Findings and fixes

Fixed during QA: dark CSS scoping; theme-click argument typing; missing CSP nonce; missing mobile section menu; collapsed desktop navigation carrying into mobile; unlabeled collapsed links; old fixed-table-column colors; missing mobile page title; modal child transition; configured gateway address handling. Incomplete local fixtures were corrected without changing production API behavior.

The reference's marketing composition is adapted through oversized real text, a particle orbit, teal emphasis and separated content chapters. Internal pages use opaque surfaces, thin dividers and immediate interactions. Existing endpoint/auth settings, data fetching and authorization remain the source of behavior.

## Quality self-assessment

| Dimension | Score / 10 | Evidence or remaining limitation |
| --- | --- | --- |
| Functionality | 9.5 | 221 relevant tests and real route/control checks |
| Code | 9.5 | Typecheck, lint, production build; existing Vue/Pinia/router patterns retained |
| UI | 9.5 | Light/dark desktop/mobile/tablet screenshots; corrected fixed-column surfaces |
| UX | 9.5 | Immediate shell/menu/dialog actions, usable mobile navigation, visible focus |
| Art direction | 9.5 | Coherent typography, orbit composition and two deliberate theme palettes |
| Performance | 9.3 | Bundle isolation and local timings verified; real-device/production p75 INP and API latency remain to measure |
| Security | 9.5 | Existing auth/URL sanitization retained, CSP nonce verified; local synthetic data only |
| Overall | 9.4 | Deliverable in the tested scope; no high-quality or full-site performance certification claimed |

Remaining follow-up: measure production interaction latency and representative low-end mobile rendering after integration, and track the 10 pre-existing test failures separately. Existing large application chunks still produce Vite size warnings; this change isolates its added visual dependency instead of refactoring unrelated modules.

final result: passed for the scoped UI acceptance criteria; full-repository test suite remains blocked by the independently reproduced pre-existing failures.

## Claude implementation continuation — 2026-10-11

This entry supersedes the earlier homepage evidence. Claude replaced the prior renderer with a native WebGL universe; the console implementation was retained. Scope: preserve that design, repair observed blockers, and provide a local interactive preview. Acceptance: build and focused tests pass; mouse parallax, chapter scrolling, both themes, mobile navigation and motion controls work in the browser. Real mobile GPU performance remains outside this desktop preview's validation.

Repairs: await Vue's DOM flush in the scene test and clean up mounts; initialize parallax despite NaN sentinel values; pause CSS nebula/chart animations; bind the light palette to the actual site theme; apply split-letter styles across Vue's component scope; restore intended spacing and button contrast; fit mobile navigation and separate the mobile hero title from body copy; start sticky chapter content at the chapter boundary. No new dependency was added.

Verification:
- 26 focused tests passed, including mouse-follow/reset regression and 21 existing home-mode checks. ESLint, vue-tsc, locale validation, production build and final CSS-only Vite rebuild passed; existing bundle-size warnings remain.
- Browser: dark/light at 1440 × 900, mobile checks at 390 × 844 and 320 × 740. At 320px the hero copy starts at y=390.9, after the title's y=324.4 bottom; no page horizontal overflow. Mobile menu opens and Escape closes it.
- Light theme resolves to rgb(238,243,246); split letters have actual inline-block layout and 0.8s/1.1s transitions. Mouse movement produces distinct depth-dependent transforms on model labels.
- Chapter navigation now places capability cards at y=394 instead of below the viewport; scrolling moves the horizontal track. Manual pause stops the renderer and pauses the nebula. Emulated reduced motion stops the renderer and removes nebula animation; emulation and viewport override were restored.
- Fresh dashboard navigation requested no HomeView/OrbitScene/universe/formations assets and mounted no particle scene. Final homepage console had no warnings or errors. Preview uses local synthetic API fixtures, not production data.
- Evidence: output/orbitix-design-review/claude-continuation-{dark,light,mobile,caps}.png; corresponding tests, lint, build and final-vite logs; console isolation JSON and a desktop interaction performance sample. Earlier renderer timing figures do not describe this version.

| Dimension | Score / 10 | Scope / remaining improvement |
| --- | --- | --- |
| Functionality | 9.5 | Focused tests and browser interactions pass |
| Code | 9.5 | Minimal fixes, cleanup regression, typecheck/lint/build pass |
| UI / UX | 9.3 | Both palettes and mobile navigation verified; smallest-screen text wrapping can be further polished |
| Art direction | 9.4 | Particle formations, glass wordmark and scroll scenes work; density and animation pacing need user preference review |
| Performance | 9.3 | Native renderer split from console and pause verified; real low-end mobile GPU/FPS not measured |
| Security | N/A | This continuation changes local visual rendering/tests, not access control or data handling; no security audit claimed |
| Overall | 9.4 | Deliverable local preview, not production performance certification |

Homepage branding follow-up: requested Sub2API display name is mapped to 小羊咩咩 in the homepage's existing name source, so its header, accessible heading, WebGL texture, particle finale and footer share the new name. Other configured site names and global console branding remain configurable. Existing 26 focused tests pass.

---

# Personal Usage Design QA

Date: 2026-10-13

Scope: the personal usage page and the shared table cells it uses. The latest
request for global UI recommendations is a separate design discussion; this
report does not claim that the whole application has been redesigned.

## Acceptance Criteria

- Show four cards in this order: billed spending, standard cost, requests,
  total tokens. Cards and logs follow the applied date and other filters.
- Remove the cache-hit-rate card and the all-dates button. The platform
  group's All control remains a group filter.
- Keep time as the first visible column. Keep first-token latency followed
  immediately by total duration. Default-hide only IP, User-Agent, and TPS;
  preserve saved user column preferences.
- Always display the 53-week heatmap independently of the main date filter.
  Model, key, and group filters still apply to its data.
- Render square heatmap cells. Hover and keyboard focus show the date,
  billed spending, and exact total tokens, including zero-usage dates.
- A single calendar date additionally shows 24 hourly bar-chart buckets.
  Use the same user timezone for query boundaries and trend aggregation.
- Verify the reference composition, chart details, responsive layouts,
  theme/language variants, loading, empty, error, retry, and keyboard use.

Completion requires passing scoped frontend checks, the database timezone
regression, rendered comparison evidence, and no remaining actionable P0/P1/P2
visual findings. Remaining performance limits must be disclosed.

## Visual Truth And Captures

Source visual truth paths:

- `C:/Users/Even/AppData/Local/Temp/codex-clipboard-3a8a9738-87f9-4256-8fd4-5e5a9c7ac23f.png`
  (full reference, 1228 x 785 pixels).
- `C:/Users/Even/AppData/Local/Temp/codex-clipboard-6a1b4918-4318-42f7-b330-1f62c5f7ecfa.png`
  (hourly chart reference, 915 x 233 pixels).

Implementation URL: `http://127.0.0.1:43179/usage`.
The preview uses a local mock API with synthetic data, not production user data.

Implementation screenshot paths, under
`C:/Users/Even/Documents/sub2api/docs/screenshots/user-usage-stats/`:

| Capture | State |
| --- | --- |
| `desktop-final.png` | Chinese, light, last seven days, request logs, all models and keys |
| `heatmap-tooltip.png` | Chinese, light, today, hover on 2026-10-13, hourly chart below |
| `hourly-desktop.png` | Chinese, light, today, 24 hourly buckets |
| `hourly-focused.png` | Actual hourly chart cropped from its document coordinates |
| `mobile-final.png` | Chinese, light, today, 390 x 844 CSS viewport |
| `mobile-en.png` | English, light, last 24 hours, corrected Standard Cost label |
| `mobile-date-picker.png` | Open date picker at 390 x 844 |
| `mobile-360-date-picker.png` | Open date picker at 360 x 800 |
| `tablet-final.png` | Chinese, light, today, 768 x 1024 |
| `tablet-1024.png` | Chinese, light, today, 1024 x 768 |
| `dark-final.png` | Chinese, dark, today, square heatmap and hourly chart |
| `empty-final.png` | Zero cards, empty heatmap, no-activity chart state |
| `error-final.png` | Synthetic 503 response, two retry controls, no stale card values |
| `loading-final.png` | Delayed heatmap/chart retry, busy state and disabled refresh |
| `tooltip-scroll-before.png` | Reproduction: focused tooltip remained after the cell scrolled away |
| `tooltip-scroll-after.png` | Fixed behavior: viewport scroll clears the tooltip |

### Viewport And Density

- Desktop override: 1228 x 785 CSS pixels; observed browser inner size:
  1228 x 786. Reported devicePixelRatio: approximately 1.
- Desktop screenshot: 1220 x 780 pixels. The host capture has a small sizing
  difference from the requested viewport. Full-view comparisons preserve both
  original pixel sizes and pad the images; they do not stretch either panel.
  Do not interpret the resulting 8-pixel width / 5-pixel height difference as
  application layout drift.
- Focused implementation chart: 916 x 237 pixels, measured CSS box
  916 x 237.6. Reference: 915 x 233. Both are shown at native pixel size;
  the 1-pixel width / 4-pixel height difference is recorded rather than hidden.
- Main desktop heatmap CSS box: 916 x 300.6. A measured day cell is
  11.7375 x 11.7375 CSS pixels; its width equals its height.
- Additional tested CSS viewports: 360 x 800, 390 x 844, 768 x 1024,
  and 1024 x 768. No document-level horizontal overflow was observed.
  The calendar and wide table use their own horizontal scrolling regions.

### Combined Comparison Evidence

The source and rendered implementation were opened together in each combined
image, not judged solely from separate screenshots.

- Full-view comparison: `comparison-final.png`, 2472 x 819 pixels.
  Both panels show the compact filter row, four metrics, annual calendar,
  and request-log navigation/table in the same order.
- Focused chart comparison: `comparison-hourly.png`, 1855 x 271 pixels.
  It confirms the title/legend arrangement, metric selector, cyan bars,
  baseline, sparse horizontal grid, and time labels.
- Pixel verification of the final focused chart found 3326 cyan pixels;
  the canvas is visibly nonblank and renders the two populated hourly buckets.

The reference uses CC Switch branding and different data. The implementation
retains Sub2API navigation, logo, account controls, and real application labels.
These differences are intentional product integration, not missing assets.

## Findings

No remaining actionable P0/P1/P2 visual findings were identified in the final
combined comparisons and tested states.

Accepted differences required by the user's latest instructions:

- The four metrics differ from the reference: spending, cost, requests,
  and total tokens replace the cache-hit-rate metric.
- There is no all-dates shortcut. The heatmap remains annual regardless of
  the main date selection, and a single date appends a separate hourly chart.
- First-token latency and total duration both appear in the table. Time stays
  first; additional default-visible fields scroll within the table.
- Keeping every requested column includes the existing aggregate token cell.
  Its additional cache-token line makes data rows taller than the reference's
  single-line rows. No content was discarded to force the old row height.
- Calendar dates use an ISO range and locale month labels; chart axes cover
  the full 24 hours, including empty hours. Mock data magnitude changes the
  y-axis scale and bar heights relative to the reference.

## Required Fidelity Surfaces

| Surface | Evidence And Result |
| --- | --- |
| Fonts / typography | The source font cannot be identified conclusively from raster alone. Implementation uses the existing system font stack with Segoe UI / Chinese fallbacks. Card values are 28px on desktop and 23px on mobile with tabular figures; labels and controls are 12-14px; section heading is 16px. Letter spacing is zero. Chinese and English mobile captures show readable wrapping without clipping. Model names retain the existing monospace style. Accepted product font integration. |
| Spacing / layout rhythm | Full comparison preserves the filter-card-calendar-log sequence, aligned content edges, restrained 8px corners, and compact metric controls. Main region gaps are 18px desktop / 14px mobile. Mobile cards form two columns. Date popovers stay within 360px and 390px viewports. Wide tables/calendar scroll inside their own regions. Passed. |
| Colors / visual tokens | Light surfaces are white with restrained gray lines; dark surfaces use near-neutral grays. Cyan chart and heatmap intensity colors follow the reference direction. Selection/focus have explicit outlines and are not indicated by color alone. Dark chart grid/text respond to theme changes. Passed. |
| Image quality / assets | The original Sub2API logo and established platform/icon components remain sharp and visible. No raster illustration in the reference was replaced with CSS artwork. The reference is a data interface; its charts are rendered with the installed Chart.js library and its calendar with native controls. No decorative images were added. Passed. |
| App copy / content | Latest Chinese metric and table labels are present, cache-hit-rate card and all-date button are absent, and tooltip contains all three requested values. English distinguishes Total Cost (billed spending) from Standard Cost (unweighted total_cost). Error and no-activity text come from existing translations. Passed. |

## Comparison And Fix History

1. Earlier requested changes corrected missing time/first-token columns,
   default column visibility, cache-hit-rate removal, the total-cost card,
   and annual heatmap independence. Earlier artifacts such as
   `all-before-latest.png` describe superseded requirements and are not final
   acceptance evidence. Latest evidence: `desktop-final.png` and
   `heatmap-tooltip.png`.
2. Date-popover overflow and rectangular calendar cells were corrected with
   a bounded popover width and square cell sizing. Post-fix evidence:
   `mobile-360-date-picker.png`, `mobile-date-picker.png`, and the measured
   equal cell width/height above.
3. [P1, data correctness] Trend aggregation previously grouped timestamps
   using the database timezone while date boundaries used the user timezone.
   Personal filtered trends now bind the user's IANA timezone in the SQL
   bucket expression. PostgreSQL integration verifies the same request at
   UTC 2026-10-12 16:30 in Shanghai and Los Angeles, for both day and hour
   grouping. Mock browser screenshots are not used as proof of this SQL fix.
4. [P2, interaction] A keyboard-focused tooltip stayed at its old screen
   position after PageDown. Before: the selected cell moved to y=-300.4 while
   the tooltip remained at y=420.14 (`tooltip-scroll-before.png`). One
   component-level scroll/resize listener now clears the tooltip, with
   unmount cleanup. The browser reproduction and regression test pass;
   post-fix capture is `tooltip-scroll-after.png`.
5. [P2, copy] Initial English mobile inspection showed two cards both named
   Total Cost. The standard-cost card now reads Standard Cost, following the
   project's existing cost terminology. Post-fix evidence: `mobile-en.png`
   and the card-label regression assertion.
6. QA evidence correction: an initial focused screenshot used viewport
   coordinates after scrolling and therefore captured the wrong document
   region. It was rejected as chart evidence. The final crop uses the
   measured document chart position (x=280, y=563.2) and was visually
   rechecked in the regenerated `comparison-hourly.png`.

## Interactions And Validation

- Applied today: $1.23 / $1.23 / 20 / 2.5M; yesterday:
  $0.95 / $0.95 / 15 / 1.8M; last seven days:
  $7.33 / $7.33 / 67 / 8.7M. Annual heatmap stays at 221.2M tokens,
  1012 requests, $206.57 for all three date selections.
- Custom October 11-12: $2.11 / $2.11 / 33 / 3.8M, still the same
  annual heatmap. These values are synthetic browser QA data.
- Claude key filter changes the annual heatmap to 42.4M / 192 / $39.31.
  Clicking today's cell retains the key filter, shows four matching logs,
  and presents the hourly chart. Model filtering similarly updates annual
  totals and selected-date cards.
- Today has 24 buckets; 09:00 has 1,242,854 tokens, 10:00 has 1,212,854.
  Other buckets are zero. Tokens/requests/cost selectors were exercised.
- Hover today shows 2026-10-13, $1.2270, 2,455,708 tokens. Requests and
  cost modes retain these three tooltip fields. An empty cell shows its
  date, $0.0000, and 0 tokens. Right-edge tooltip remains inside the viewport.
- Arrow-key focus, Escape dismissal, future-day disabling, and viewport
  scroll dismissal were verified in component tests and browser interaction.
- Column menu: every toggle is on except TPS, IP, and User-Agent. Time is
  outside the hideable set and remains the first column.
- Empty, delayed, and synthetic 503 states were exercised. Failure replaces
  stale cards with placeholders and offers heatmap/chart retries. Both
  retries return to populated data; refresh restores cards as well.
- Browser console after a fresh normal reload: no new error/warn entries.
  Errors intentionally generated by the synthetic 503 checks are excluded.
- Final frontend validation: 82 tests in six files plus 2 usage-entry tests,
  84 total, passed. Scope includes UsageView, heatmap, trend, shared table,
  date picker, locale-key completeness, and usage entry routing.
- ESLint for changed Vue source passed. Final production build, including
  TypeScript checking, exited 0 (`build.log`, built in 32.93s).
- Go trend/query unit regressions passed. Real PostgreSQL timezone test:
  `go test -tags integration ./internal/repository -run '^TestUsageLog_PersonalTrendTimezoneAtDateBoundary$' -count=1`
  passed. Only task-created testcontainers were removed afterward.
- `git diff --check` passed. No new application dependencies were introduced.

## Quality Assessment

These are scoped engineering judgments supported by the checks above, not
measurements of every page or a production performance guarantee.

| Dimension | Score | Evidence / Limit |
| --- | --- | --- |
| Functionality | 9.6/10 | Date/filter/column/tooltip checks, 84 frontend tests, real SQL timezone regression |
| Code | 9.5/10 | Existing APIs/utilities/chart dependency reused, scoped edits, lint/type/build checks |
| UI | 9.5/10 | Native-size combined comparisons and desktop/mobile/tablet/theme captures |
| UX | 9.5/10 | Keyboard access, stable date behavior, preserved preferences, explicit recovery states |
| Art Direction | 9.5/10 | Cohesive neutral surfaces and cyan data marks, retained product branding |
| Performance | 9.4/10 | One tooltip, 371 cells, 24 chart buckets, animations disabled; real large annual query latency remains unbenchmarked |
| Security | 9.5/10 | Existing authenticated user API path retained; timezone is a bound SQL argument; no external data transfer added |
| Overall | 9.4/10 | Deliverable in the verified scope; large real-data performance still needs measurement |

Overall: deliverable. Improvement below 9.5: benchmark the annual aggregate
on realistic high-volume data before making broader performance claims.
The existing build also reports large bundled chunks; this page change does
not perform an unrelated bundling refactor.

## Implementation Checklist

- [x] Latest metric, date, column, chart, and hover requirements verified.
- [x] Source and implementation full-view / focused comparisons opened.
- [x] Applicable typography, spacing, color, asset, and copy surfaces reviewed.
- [x] Responsive, language, theme, keyboard, error, loading, empty, and retry states checked.
- [x] Scoped tests, type/build/lint checks, SQL timezone regression completed.
- [x] Remaining performance limitation stated.

final result: passed


## 2026-10-27 月账单补充验收

保留以上既有验收记录。本次月账单的三列模型汇总、动态站点名称、仅 PDF 导出，以及桌面/手机/打印与金额权限验证详见 [月账单验收](docs/monthly-bills-qa.md)。

final result: passed
