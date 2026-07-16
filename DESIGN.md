# Design

## Visual Theme

Production operations console for a risk-control adapter. The physical scene is: an administrator on a bright desktop display during a pre-block rollout, scanning for latency, fail-open and misclassification risk. The UI should feel calm, legible and accountable.

## Color Palette

Use OKLCH custom properties. Strategy: restrained product UI, pure white content surface, cool neutral workspace, crimson-orange used only for primary action, risk and active selection.

```css
:root {
  --bg: oklch(0.982 0 0);
  --surface: oklch(1 0 0);
  --surface-raised: oklch(0.965 0.006 250);
  --ink: oklch(0.19 0.018 250);
  --muted: oklch(0.45 0.02 250);
  --line: oklch(0.88 0.012 250);
  --primary: oklch(0.58 0.19 34.8);
  --primary-hover: oklch(0.52 0.20 34.8);
  --accent: oklch(0.47 0.13 205);
  --success: oklch(0.52 0.13 145);
  --warning: oklch(0.72 0.16 78);
  --danger: oklch(0.55 0.20 28);
  --info: oklch(0.56 0.12 240);
}
```

## Typography

System sans stack: `Inter`, `ui-sans-serif`, `system-ui`, `-apple-system`, `BlinkMacSystemFont`, `Segoe UI`, `sans-serif`. Fixed rem scale, no fluid product typography. Headings are compact and functional; labels and table data are optimized for scan speed.

## Components

Use a left navigation app shell with a top status bar. Buttons are 6px radius; cards/panels are at most 8px radius; no nested cards. Use standard controls: switches for booleans, segmented controls or selects for enums, numeric inputs/sliders for rates and TTLs, tables for mappings, tabs or side nav for sections.

## Layout

Desktop-first operations layout with responsive collapse for smaller screens. Keep tables dense but readable. Use full-width panels and constrained content, not decorative floating sections.

## Motion

Transitions are limited to 150-200ms hover/focus/state feedback. No page-load choreography. Respect `prefers-reduced-motion`.

