# 0003. Contract-first OpenAPI with codegen on both sides

Status: accepted

## Context

A TypeScript client and a Go server must agree on every field. Hand-written
types on both sides drift. The API will be built by several workers in
parallel, so the contract has to exist before the implementations.

## Decision

`api/openapi.yaml` (OpenAPI 3.1) is the single source of truth. Both sides are
generated from it and the output is committed:

- Go: `oapi-codegen` writes a strict `net/http` server interface and models into
  `server/internal/httpapi/api.gen.go`.
- TypeScript: `openapi-typescript` writes `packages/api-client/src/schema.d.ts`;
  `openapi-fetch` provides the typed client.
- MSW mock handlers are typed against the generated types.

`npm run gen` regenerates everything. CI runs it and fails if
`git status --porcelain` is non-empty.

Contract rules: success responses are `{data, meta}` with `meta.dataAsOf`,
`meta.serverTime`, `meta.stale` and, where relevant, `meta.scoreState`;
errors are RFC 9457 `application/problem+json` with a `requestId`; lists use
cursor pagination; every enum is open (clients tolerate unknown values);
public ids are opaque prefixed ULIDs, never FPL integers (ADR 0006);
variants are `oneOf` with a `discriminator` (matchup sides, transactions).

## Consequences

- A contract change shows up as compile errors on both sides.
- Generated files are committed, so reviews show contract changes twice. That
  is intended.
- oapi-codegen does not validate request constraints (min, max, pattern) by
  itself. Handlers validate ids and ranges today. A Go test validates every
  operation's responses against the spec with libopenapi-validator, which
  supports OpenAPI 3.1 and JSON Schema 2020-12.
- Auth is not in the contract yet. Adding it is a contract change.
