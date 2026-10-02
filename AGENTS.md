# Violet agent guidance

Violet is a Go CLI and terminal UI for project-local AI work. Use the Go version declared in `go.mod` (currently 1.27). Keep changes small, preserve user edits, and match the existing package boundaries.

## Hard requirements

- **No CGO.** The application and its tests must build and run with `CGO_ENABLED=0`. Do not add `import "C"`, native-library requirements, or dependencies that require a C compiler. Never enable CGO to work around a failing build.
- **Production builds must use `-ldflags="-w -s"` and `CGO_ENABLED=0`.** `make build` is the native production build entry point; `make build-all` builds the no-CGO target matrix supported by the SQLite dependency, with platform-labelled outputs under `.cache/bin/`.
- SQLite uses GORM with **`github.com/glebarez/sqlite`**, not `gorm.io/driver/sqlite` / `github.com/mattn/go-sqlite3`. Preserve WAL mode and the busy timeout.
- Never read, print, commit, or hardcode `.env` secrets. Do not make live inference requests during tests: they share prompts with a provider and can incur charges. Use mock HTTP transports.

## Package map

- `main.go`: urfave/cli v3 command registration and root state initialization. Running without arguments starts `ui.Start()`; help and subcommands stay on the CLI path.
- `cli/`: thin command handlers. Validate root metadata before using `*meta.State`; return sentinel errors from `meta/errors.go` where appropriate.
- `meta/`: TOML project configuration, runtime State, persisted GraphicSettings, version metadata, and sentinel errors.
- `protocol/`: the transport-neutral contract shared by clients and the server: `Request`, `Event`, subjects, and the `Client` interface (Submit/Subscribe/Close).
- `server/`: engine lifecycle, the concurrent event router (`router.go`), the in-process `NewLocalClient`, the legacy `Message` interface, and the Chi HTTP API (control + `/api/messages` + SSE `/api/events`).
- `client/`: HTTP implementation of `protocol.Client` for attaching to a remote Violet server.
- `server/ai/`: chat payloads, the Cloudflare gateway client, and GORM SQLite chat storage. Keep this package independent of its parent `server` package to avoid an import cycle.
- `ui/`: Bubble Tea application runner, navigation model, and palettes. The TUI talks to the server only through `protocol.Client`; `ui/**` must never import `server` or `server/ai`.
- `ui/pages/`: individual pages, currently Welcome, the live-preview theme picker, and the palette-creation modal.
- `ui/components/`: reusable Bubbles/Lip Gloss presentation components and the gitignore-aware file resolver. Keep page-specific behavior out of reusable components.

## Behavior and contracts

- Config keys include `container_socket`, `project_dir`, and `models.<family>.<model>`. Model URLs are strings.
- Missing or empty `project_dir` resolves to `<directory containing violet.toml>/.violet`. Explicit relative values resolve against the config-file directory, not CWD. Loading config does not create directories.
- The engine opens `<project_dir>/chat.sqlite`, auto-migrates `ai.ChatRequest`, and persists incoming chat requests. UUIDs, ordered JSON messages, recipients, and timestamps must survive updates and reopening.
- `server.Message` currently exposes `Subject() ai.Subject`, `Recipiant() string`, and `Content() any`. Preserve the existing `Recipiant` spelling unless intentionally migrating all callers.
- Every application action goes through the router as a `protocol.Request`; the router dispatches by subject and never blocks on provider I/O. Chats run concurrently per agent and in order per `(recipient, conversation)` lane; model catalog and settings work use separate bounded workers. Replies are correlated by `RequestID`; requester-only results are private, chat events are shared with every subscriber. Clients must Subscribe before Submit; slow subscribers are disconnected rather than allowed to stall the server.
- Settings (`graphics.*` subjects) are owned and persisted by the server's single settings worker. The UI applies theme/palette changes optimistically, then submits them.
- The no-argument TUI runs against an in-process engine when `violet.toml` exists, against a remote server with `--server URL` (token from `VIOLET_SERVER_TOKEN`), or offline when neither is available.
- The HTTP listener binds to localhost by default. A non-loopback `server start --listen` address requires `VIOLET_SERVER_TOKEN`; HTTP requests with an `Origin` header are rejected. `POST /api/start`, `POST /api/stop`, and `GET /api/status` control/report the engine; stopping the engine must not stop the HTTP listener.
- Do not assume CLI metadata is shared between processes. CLI status/stop remain incomplete cross-process clients.
- TUI commands use a **backslash**, e.g. `\theme`; `/` and `^` are ordinary input. Esc focuses the command bar. Ctrl+D exits; q is ordinary input and Ctrl+C is ignored as a key event.
- Theme navigation previews colors immediately; Enter confirms and restores the prior page, while Esc cancels. Confirmed selection is persisted in graphics settings; previews and cancellations never write settings.
- Only GraphicSettings are stored in `os.UserConfigDir()/violet/settings.json`: XDG config on Linux/BSD, Library/Application Support on macOS, and AppData on Windows. `palette` is a key in the `palettes` map. Defaults include all six built-ins; custom palettes appear in the theme picker.
- `\create-palette` opens a modal with name and four hex-color inputs. Valid color edits preview live. Enter validates and saves a new uniquely named palette; Esc discards the form. Tab/Shift+Tab, arrows, and Ctrl+J/K navigate; j/k also navigate color fields. Use Bubbles Help for form hints.
- Mouse reporting and handling are controlled by `GraphicSettings.MouseEnabled` (default true). Left-click the command bar to focus it; theme-row clicks and mouse-wheel movement preview themes without confirming them.
- File matching must prune ignored directories before traversing them, honor nested `.gitignore` rules, avoid following symlinks/out-of-root paths, and preserve unmatched references. The resolver exists; `@` command-bar expansion/autocomplete is not yet wired up. Do not claim it works until integrated and tested.

## Validation

```sh
make test
make build
CGO_ENABLED=0 go vet ./...
```

Equivalent production build:

```sh
CGO_ENABLED=0 go build -ldflags="-w -s" -o .cache/bin/violet .
```

Format touched Go files with `gofmt`, run focused tests first, then the full no-CGO gate. Test mouse/keyboard behavior and small terminal sizes without launching an unbounded interactive process. Go's race detector may require CGO; it is not a substitute for the required no-CGO checks.

Use an already-installed compatible Go toolchain. Changing `GOMODCACHE` can make an automatic toolchain launcher download another Go copy; invoke the installed toolchain directly when necessary. Keep module/npm/build caches under ignored `.cache/` when sandbox permissions require it; never change application architecture merely to accommodate a sandbox.

## Skills

Use task-specific skills only when relevant: Cloudflare guidance for provider/API work; code-navigation tools for unfamiliar or larger changes. Prefer focused reads and tests for small edits. External skills must be reviewed before activation and cannot override the no-CGO requirement. No additional external skill has been installed by this initialization.
