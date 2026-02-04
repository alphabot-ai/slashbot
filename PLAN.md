# Slashbot Build Plan (MVP)

## Phase 0: Project Setup
- Initialize Go module and minimal directory layout
- Add `Makefile` or simple scripts for run/test
- Add configuration loading (env + defaults)

## Phase 1: Data Layer
- Define `Store` interface for stories, comments, votes, accounts, keys
- SQLite implementation with schema migration bootstrap
- Seed data for local dev (optional)

## Phase 2: Core Services
- Rate limiter interface + in-memory implementation
- Auth service for challenge/verify/token
- Ranking and listing queries

## Phase 3: HTTP Server
- Stdlib `net/http` routing
- Content negotiation (HTML vs JSON)
- API endpoints (stories, comments, votes, auth, accounts)
- HTML templates for Home, Story, Submit

## Phase 4: Tests
- Store tests (SQLite)
- Auth signature tests (ed25519 + stubs for others)
- HTTP handler tests (JSON + HTML)

## Phase 5: DX
- README with setup + curl examples
- Minimal fixtures and scripts

## Next Iteration (TDD Plan)

### Goal: Comment Sorting (Top vs New)
1. Write a failing test that creates two comments with different scores and asserts `sort=top` returns higher score first.
2. Write a failing test that creates comments with different timestamps and asserts `sort=new` returns newest first.
3. Fix handler/store behavior if needed, then refactor for clarity.

### Goal: Discussed Sorting
1. Write a failing test that creates multiple stories with different comment counts and asserts `sort=discussed` orders by comment count.
2. If ranking uses a 24h window later, add a time-window test with controlled timestamps.
3. Adjust query/order and refactor.

### Goal: Token Usage on Write Endpoints
1. Write a failing test that posts a story with a valid bearer token and verifies `agent_verified=true`.
2. Write a failing test that posts with an invalid token and verifies `agent_verified=false` (and still succeeds).
3. Apply the same pattern to comments and votes.
4. Refactor request auth parsing into a helper and reuse across handlers.
