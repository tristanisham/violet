# Vercel AI Gateway provider: implementation plan

Status: approved design (sections 1–3, config amendment, Stringer), not yet implemented.
Base commit: `1de2103`.

## Goal

Add Vercel AI Gateway as a second chat/catalog provider next to Cloudflare, behind a
typed `Provider` interface and registry owned by the server. Clients pick a provider
explicitly per chat; the server reports which providers exist, which are usable, and
which is the configured default.

## Settled decisions

| Question | Decision |
| --- | --- |
| How a chat picks a provider | Explicit `provider` field on `protocol.ChatInput`, persisted on `ai.ChatRequest`. |
| Who decides availability | The server, through a new `providers.list` subject. The TUI renders what the server reports. |
| Provider layer shape | `Provider` interface + immutable `Registry` in `server/ai`; one file per provider. |
| Display name | `Provider` embeds `fmt.Stringer`; `String()` is the display name (no separate `Name()`). |
| Default provider | `[providers.<id>] default = true` in `violet.toml`; `cloudflare` when none is marked. |

## Facts verified before writing this plan

- `GET https://ai-gateway.vercel.sh/v1/models` returns 200 **without credentials** (checked
  2026-10-02, no key sent). Body is `{"object","data":[...]}` with 406 entries; every entry has
  `type`, and 267 are `language`. Entries carry `id` (e.g. `alibaba/qwen-3-14b`), `name`
  (display, e.g. `Qwen3-14B`), `description`, `context_window`, `max_tokens`, `tags`,
  `modalities`, and sometimes `deprecated_at` (epoch **milliseconds**). No `language` entry
  currently has `deprecated_at`.
- Chat is OpenAI-compatible: `POST /v1/chat/completions`, `Authorization: Bearer
  $AI_GATEWAY_API_KEY`, body `{model, messages}`.
- `main.go` imports `github.com/joho/godotenv/autoload`, so `.env` (which now holds
  `AI_GATEWAY_API_KEY`) is loaded into the process environment for the binary. Test binaries
  do not import `main`, but a developer shell may still export the key, so tests must clear it.
- Today `ai.CatalogModel` and `protocol.CatalogModel` are separate structs converted with
  `protocol.CatalogModel(model)` in `server/engine.go`. Adding fields to only one breaks that
  conversion.
- **Field-meaning mismatch:** Cloudflare's catalog `id` is a UUID and its `name` is the model
  path used for inference (`@cf/...`). Vercel's `id` is the inference identifier and its `name`
  is a display label. The TUI currently renders `Name`. Without normalization, a client cannot
  know which field to put in `ChatInput.Model`.

## Correction to the previous session

The last summary said "the config change and the stringer are both in." That was wrong: both
were agreed **in the design** only. `meta/config.go` has no `Providers` field and no `Provider`
type exists. Both are tasks below.

## Wire contract changes (`protocol/protocol.go`)

```go
const SubjectProviders Subject = "providers.list" // add to Validate's switch

type ChatInput struct {
    Provider string        `json:"provider,omitempty"` // empty → server default
    Model    string        `json:"model"`
    Messages []ChatMessage `json:"messages"`
}

// CatalogModel: ID is always the identifier to send as ChatInput.Model.
type CatalogModel struct {
    Provider      string `json:"provider"`
    ID            string `json:"id"`
    Name          string `json:"name"`
    Description   string `json:"description"`
    ContextWindow int    `json:"context_window,omitempty"`
}

type ProviderInfo struct {
    ID        string `json:"id"`
    Name      string `json:"name"`      // Provider.String()
    Available bool   `json:"available"` // credentials present on the server
    Default   bool   `json:"default"`
}
```

- `CatalogModel.Task` is removed from the wire type; it was Cloudflare-specific and nothing in
  `ui/` reads it. Cloudflare decodes into a private struct and filters on it there.
- `ai.CatalogModel` becomes `type CatalogModel = protocol.CatalogModel` (alias, like
  `ai.ChatMessage`), deleting the struct cast in `engine.go`.
- Normalization: Cloudflare sets `ID = Name = <name>` (the `@cf/...` path) and drops the UUID;
  Vercel sets `ID = id`, `Name = name`. This is a deliberate change to Cloudflare's `ID` on the
  wire; no current consumer uses the UUID.

## Task 1: config (`meta/`)

Files: `meta/config.go`, `meta/config_test.go`, `meta/errors.go`, `cli/init.go`.

```go
type ProviderConfig struct {
    Default bool `json:"default" toml:"default"`
}
// Config gains:
Providers map[string]ProviderConfig `json:"providers" toml:"providers"`

const DefaultProviderID = "cloudflare"
func (c *Config) DefaultProvider() string // the key marked default, else DefaultProviderID
```

- `NewConfig` rejects keys that are not lowercase (`[providers.Vercel]`) and more than one
  `default = true`, wrapping a new sentinel `ErrInvalidProviders` with the offending keys.
- Unknown IDs are **not** rejected in `meta` (it doesn't know what the server implements).
- `violet init` keeps writing `meta.Config{}`; verify the marshalled stub stays valid TOML with
  a nil map (add a test that `init` output round-trips through `NewConfig`).
- Tests: no table → `cloudflare`; `vercel` marked → `vercel`; `Vercel` key → error;
  two defaults → error; listed without `default` → still `cloudflare`.

## Task 2: provider contract and registry (`server/ai/provider.go`)

```go
type Provider interface {
    fmt.Stringer                 // display name: "Cloudflare", "Vercel"
    ID() string                  // stable lowercase key
    Available() bool             // credentials present for chat
    ListModels(ctx context.Context) ([]CatalogModel, error) // text generation only
    Chat(ctx context.Context, model string, msgs []ChatMessage) (json.RawMessage, error)
}

type Registry struct{ /* immutable, sorted by ID */ }
func NewRegistry(providers ...Provider) (*Registry, error) // rejects duplicate/empty/non-lowercase IDs
func DefaultRegistry() *Registry                            // cloudflare + vercel from env
func (r *Registry) Get(id string) (Provider, bool)
func (r *Registry) List() []Provider
```

- Shared helper `readJSONBody(resp, limit)` (8 MiB cap, `json.Valid` check) replaces the copy in
  `engine.go`'s `runChat`.
- Shared `newHTTPClient()`: 2-minute timeout, `CheckRedirect` returns `http.ErrUseLastResponse`.
  Each provider owns its own client so one hung provider cannot exhaust another's connections.
- Tests: duplicate IDs rejected; `List` sorted; `Get` miss.

## Task 3: Cloudflare adapter (`server/ai/cloudflare.go`)

- Wrap the existing `AiGateway`. Construction no longer fails on missing env: it records
  `Available() = account ID and token both non-empty`; `ListModels`/`Chat` return
  `"cloudflare: CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN are required"` when unavailable.
- `ListTextGenerationModels` decodes into a private `cfModel` (with `Task`) and maps to
  `CatalogModel{Provider:"cloudflare", ID:name, Name:name, Description}`.
- `Chat` = today's `runChat` body (calls `Run`, reads via `readJSONBody`).
- Existing `ai_gateway_test.go` tests keep passing; add: unavailable without env, mapping of
  `ID`, `String() == "Cloudflare"`.

## Task 4: Vercel adapter (`server/ai/vercel.go`)

- Base URL `https://ai-gateway.vercel.sh/v1`; key from `AI_GATEWAY_API_KEY` (trimmed), read once
  at construction. `Available() = key != ""`. `String() == "Vercel"`.
- `ListModels`: `GET /models`, 20 s timeout, 8 MiB cap. Bearer header only when a key is set
  (catalog is public). Keep `type == "language"` and drop entries whose `deprecated_at`
  (ms) is in the past. Map to `CatalogModel{Provider:"vercel", ID:id, Name:name, Description,
  ContextWindow}`, sort by `ID`. Skip entries with empty `id`. If filtering leaves zero
  models, return an error instead of an empty list.
- `Chat`: unavailable → error without any request. Otherwise `POST /chat/completions` with
  `{"model":model,"messages":msgs}`; return the raw JSON response.
- Errors: `vercel: HTTP <code>` plus a fixed hint for 401/403 (check `AI_GATEWAY_API_KEY`) and
  429 (rate limited). Never include response bodies or the key.
- Tests (all via `httptest.Server` or `roundTripFunc`, with `t.Setenv("AI_GATEWAY_API_KEY", ...)`
  set explicitly, including `""`):
  - catalog filter (language only, past-deprecated dropped, future-deprecated kept), sorting,
    mapping; fixture trimmed from the real response shape above;
  - bearer header present only with a key;
  - chat request method/path/body/headers; no request made when unavailable;
  - 401/429 mapping; key absent from every error string;
  - redirect not followed; oversize body rejected; non-JSON body rejected.

## Task 5: engine and router (`server/`)

Files: `engine.go`, `router.go`, `router_test.go`, `http_test.go`, `client/http_test.go`.

- `Engine` loses `LoadModels`/`RunChat`; gains `Providers *ai.Registry` (nil →
  `ai.DefaultRegistry()` at `Start`). `Start` resolves `state.Config.DefaultProvider()` and
  fails if that ID, or any key in `state.Config.Providers`, is not in the registry; the error
  lists the known IDs.
- `engineRuntime` holds `providers *ai.Registry` and `defaultProvider string`.
- Routing:
  - `providers.list` → settings worker; private `[]ProviderInfo`; no network.
  - `models.list` → model workers; provider = `Recipient` or default; unknown → private error;
    stamp `Provider` on every model before replying.
  - `chat` → lanes keyed `(agent, conversation)` as today, **not** by provider. In
    `handleChat`, resolve provider before `store.Create`: unknown or `!Available()` → private
    error and nothing saved. Then save → `chat.saved` → `provider.Chat` → `chat.completed`.
  - An empty `ChatInput.Model` is an error for every provider except Cloudflare, which keeps
    `ai.DefaultModel` for compatibility.
- `ai.ChatRequest` gains `Provider string` (`json:"provider,omitempty"`); `AutoMigrate` adds the
  column. Rows with an empty value read as `cloudflare`. Legacy `Message` path sets
  `cloudflare`. Add a store test that an old-schema database reopens and migrates.
- Test migration: replace `fakeGateway` hooks in `server/router_test.go` and
  `client/http_test.go` with a `fakeProvider` implementing `ai.Provider`, injected via
  `ai.NewRegistry`. Do this in one task so nothing is half-migrated.
- New router tests: routing by provider; configured default honored for chat and models;
  unknown provider and unavailable provider fail privately **before** saving (assert store
  empty); `Provider` persisted; a blocked provider does not block another provider's chat in a
  different lane; `providers.list` reply is private, sorted, with `Default` set.
- Guard test: `NewEngine()` used in tests must never reach `DefaultRegistry()` with network;
  every test helper injects a registry.

## Task 6: TUI (`ui/`)

Files: `ui/model.go`, `ui/client.go`, `ui/pages/providers.go`, tests.

- Remove the hardcoded provider list in `NewProviders`. On open the root submits
  `providers.list`; the page shows the reply alphabetically, greys out `!Available`, marks the
  default, and initially selects the default.
- Selecting an available provider submits `models.list` with `Recipient = provider ID` and a
  fresh request ID; stale replies (other ID or other provider) are ignored.
- Model rows show `Name`, falling back to `ID`; the page keeps `ID` as the value for chats.
- `decodeModels`/a new `decodeProviders` sanitize every server string (existing `sanitize`).
- Offline / disconnected behavior unchanged: page shows the offline error, no submit.
- `ui/**` still must not import `server` or `server/ai` (existing import test enforces it).
- Tests: providers rendered from a fake reply; unavailable not selectable; default marked;
  selecting submits `models.list` with the right recipient; stale replies dropped; existing
  mouse/size tests updated.

## Task 7: docs and validation

- `AGENTS.md` (and the identical `CLAUDE.md`): add `providers.<id>.default`, the `providers.list`
  subject, `AI_GATEWAY_API_KEY`, and "provider adapters live in `server/ai`, one file each".
- Gate, no CGO, using the installed toolchain:

```sh
gofmt -l .
CGO_ENABLED=0 go vet ./...
make test            # then: CGO_ENABLED=0 go test -count=5 ./server/... ./client/... ./ui/...
make build
make build-all
```

- No test may hit the network: run the suite once with `HTTPS_PROXY=http://127.0.0.1:1` and
  `AI_GATEWAY_API_KEY`/`CLOUDFLARE_*` exported to dummy values; it must still pass.
- Code review subagent over the full diff before committing; fix findings; then commit.

## Ordering and parallelism

1 → 2 → (3 ∥ 4) → 5 → 6 → 7. Tasks 3 and 4 touch disjoint files and can run as parallel
subagents; 5 must land in one piece because removing the hooks breaks every test helper at
once; 6 depends on the final wire types from 5.

## Open risks

- `deprecated_at` semantics are inferred from four non-language entries (future timestamps in
  ms). If Vercel changes the unit, the filter could drop everything; the test fixture pins the
  current shape, and a catalog that filters to zero models returns an explicit error rather
  than an empty list.
- Changing Cloudflare's `CatalogModel.ID` from UUID to model path is a wire change. Any external
  client relying on the UUID breaks; none exists in this repo.
- Vercel chat responses are returned raw (OpenAI shape) while Cloudflare's are Workers AI shape.
  No page renders chat output yet; normalizing responses is out of scope and should be decided
  before a chat page is built.
