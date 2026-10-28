# 0001. Expo universal app, web first

Status: accepted

## Context

The product is a Sleeper-style companion for FPL Draft leagues. The target is a
public App Store app (and later Google Play). No launch date is set. We want to
see the product in a browser first, because it is the fastest way to put it in
front of a league.

An existing EAS CI/CD pipeline (a friend's) is available to build and ship
native binaries and OTA updates later.

Options considered:

- A Vite SPA for web, then a separate native app later.
- One Expo (React Native) codebase that targets web, iOS and Android.
- Flutter or SwiftUI. Ruled out in RESEARCH.md section 9.2.

## Decision

One Expo app (`apps/app`, TypeScript strict, Expo Router) is the only client.
We build and ship the web version first (`expo export --platform web`, with
`web.output: "single"`, a client-routed SPA). iOS and Android come from the
same code later through the existing EAS pipeline.

We chose Expo over a Vite SPA because the destination is a public App Store
app and EAS is already available. A Vite SPA would be thrown away or rewritten
at that point.

Styling is React Native `StyleSheet` plus a typed design-tokens module. The
layout is responsive: a bottom tab bar under 768px wide, a sidebar above.

## Consequences

- Web, iOS and Android share routes, state and components. Platform splits use
  `.web.ts` / `.native.ts` files only where needed (the MSW bootstrap, the
  query focus/online wiring).
- The web build is served same-origin with the API, so it has no API URL
  baked in; native builds must set `EXPO_PUBLIC_API_BASE_URL`.
- `public/index.html` is the web template (`viewport-fit=cover`, so safe-area
  insets work on notched phones).
- Mocks (MSW) are compiled only into `build:web:mocks` (`dist-mocks`); the
  production build is checked for leaks by `npm run verify:prod-bundle`.
- We accept React Native Web's quirks and a larger web bundle than a hand-built
  SPA.
- The web build is a single `index.html`, so a host must serve it for every
  path (SPA fallback).
- `eas.json` has development, preview and production profiles but no project
  id or credentials. Bundle identifiers are placeholders until the owner
  connects the pipeline.
- Target is a public App Store app. No launch date is set; web is a stepping
  stone, not the product.
