# Browser and terminal test-build verification

September 14, 2026. Development host: macOS arm64, Go 1.27.0. Source declares Go 1.24 as its minimum; the actual compiler used was 1.27.0.

## Automated checks

The full test suite across six packages passed with the race detector, followed by `go vet ./...`. The suite ran through `make test`; subsequent targeted checks covered metadata refresh, automatic budgets, and separate connection/model saves. Each package has a 60-second test timeout; UI waits and fake network startup have shorter bounds.

- File store: private-note exclusion, leaf-to-parent conversion, inherited settings/attachments, stale external saves, invalid tree reload, single-process locking, failed-open cleanup, protected authored prose, stale candidate rejection, and changes discovered after restart.
- Change classification: setup/planning saves need no prose review; active prose and retained draft candidates do. Operational configuration changes preserve candidate/proposal freshness, while writing inputs and effective model selections request review. External operational edits still require Reload. Restart resolves the three obsolete model-setup prompts without changing the project or duplicating history; unchanged legacy cached-run fingerprints survive upgrade. No-op configuration saves produce no decisions.
- Assistant failure regression: a single-outline project reaches the real HTTP adapter in one call without pointless context selection. A held response exposes the conversation/run identity, call count, and progress; an empty `finish_reason: length` response persists an actionable failure. An explicit retry uses an increased output limit and retains both turns. Console lifecycle logs omit the prompt. Older conversations recover failed status from their saved runs.
- Application service: queues complete and activate prose with no UI and with an unread subscriber; following passages see prior committed prose. Shared call budgets, authored-text protection, cancellation interlocks, shutdown lock retention, stable conversation completion, preserved draft input, stale text/config saves, detached view data, proposal validation before writes, outline import/invalidation, automatic generation, model capability validation, project switching and subscription cleanup are exercised without terminal dependencies. Connection-test cancellation holds the same editing interlock until completion. A package-boundary test prevents either UI from owning storage or importing the engine/model adapter.
- Model HTTP adapter: endpoint/model/token-field contracts, URL normalization, compatible and native discovery, optional/unknown capabilities, authentication, empty lists, error redaction, rejected redirects, bounded responses, token-parameter negotiation, validated tool arguments/continuations, cancellation, and preservation of token-truncated text.
- Browser HTTP API: embedded assets, source/config stale-write rejection, pending-change admission, generation, full review records, candidate activation/invalidation, model setup followed by first-draft generation without review prompts, loopback and wildcard IPv4/IPv6 binding, hostname/IP requests for assets and API commands, Origin/fetch-header checks, malformed requests, SSE connection/disconnection, cancellation checkpoints, and editing interlocks.
- Generation engine: a local HTTP fixture performs selection tools, fails an initial consistency review, receives a revised candidate, and passes consistency/style. Other tests cover call exhaustion, unknown tools, out-of-scope/private-note edits, malformed verdicts, and cancellation with a completed draft retained. Fixtures do not call a hosted or local LLM.
- Terminal simulation at 120×40 and 80×24: real mouse events select sources and editor buttons; modifier keys save and find commands; compact navigation generates a passage and persists its operation. A scoped edit conversation remains attached to its original source while browsing changes, and Apply changes only that source. Cancellation clears queued work and releases editing only after the active call stops.
- Interface lifecycle: detaching the terminal leaves worker completion independent of rendering; outline completion opens its inspector; cancel-and-quit preserves both the canceled turn and the unsent conversation draft. The cancel-and-quit test caught a draw-lock deadlock during the refactor; completion actions now run in event handling, and terminal teardown has a bounded wait.

The interaction tests exposed and now cover container replacement during TreeView drawing, rapid clicks on different buttons being misclassified as double clicks, and missing keyboard forwarding through dialog overlays. Long help content uses a scrollable view.

## Browser interaction checks

The actual embedded interface was exercised in the in-app browser at 1280×720, with smaller-window checks at 1024×768 and 390×844. Browser assets are plain embedded HTML/CSS/JavaScript; the destination needs no frontend runtime.

- Navigate the outline and world wiki; edit/save a passage, review its invalidation choices, and generate prose through the real application service with the explicit demo client.
- Inspect a candidate, its consistency/style findings, and its retained exact context/tool records.
- Start a scoped edit conversation, send a prompt, browse another passage, retain an unsent follow-up, inspect before/after text, and apply the proposal to its original source. The other viewed passage remains unchanged.
- Against a local deterministic HTTP server, enter only its URL, discover three model choices with LM Studio-shaped metadata, exclude embeddings, classify a text-only model, and save it without assigning it as base. Test and save a tool-capable base model with automatic `max_completion_tokens` negotiation. These browser tests used the real HTTP adapter, not the demo discovery shortcut.
- Assign the text-only model to style; check that tool-required selectors offer only the verified tool-capable model. Verify unsaved settings prompt before navigating away.
- Inspect the desktop layout and compact navigation. Check browser console errors during the completed workflow.
- Against a delayed local HTTP fixture, send an assistant message on a single-point outline, inspect the active stage and elapsed time in both workspace and assistant, then return an empty token-limited reply. Verify persistent failure details and Retry message. Retry returns a visible outline reply. A separate edit session invokes `propose_edit`; inspecting and applying it replaces the intended outline with seven bullet points. No hosted or local LLM is called by this fixture.

These are functional interaction checks, not an author usability study. Model fixture outputs and metadata are synthetic; those fixture checks did not call an LLM. Separate live checks are recorded below. The discovery and capability tests establish protocol behavior against fixtures, not the truth of every server's reported metadata.

## Release checks

Release artifacts built successfully with `CGO_ENABLED=0` and `-trimpath`. The `file` utility identifies the Linux outputs as statically linked x86-64 and aarch64 ELF executables; the native output is macOS arm64 Mach-O. Archive contents include the executable, documentation, and dependency notices. Native creation/check and zero-directory-argument HTTP startup passed. An ephemeral loopback listener served embedded assets and the expected current project. SIGTERM shut down cleanly and released the project lock; native CLI checks also bound to wildcard IPv4, all interfaces, and an explicit LAN IP, then loaded assets/state and saved an edit through the real LAN address. Each shutdown released the project lock. `--version` reports 0.2.0-test. See `dist/SHA256SUMS` for artifact hashes. Linux binaries were cross-compiled, not executed on Debian here.

`python3 scripts/smoke-pty.py dist/iterauthor` passed against the compiled native app through a Unix pseudo-terminal with `TERM=xterm-256color`. It used actual SGR mouse reports to select a scene, open its editor, and place the caret inside the text; typed content was saved to the expected file. Command search and Help used modifier keys, and Ctrl+C exited successfully. The harness drains terminal output through exit and uses bounded waits. Python is a development-test dependency only.

The preceding 0.1 PTY checks also covered a zero-directory-argument launch from an existing project directory; the 0.2 harness explicitly selects `--tui`. CLI checks covered creation and validation using the current directory, an explicit path overriding the current directory, optional-directory help text, and rejection of extra paths or `--sample` without `--new`.

## Not established by these checks

- Actual iTerm → SSH → Debian operation, SSH tunnel configuration, or behavior of a specific tmux setup.
- Live Ollama, llama.cpp, or hosted model compatibility. Their contracts were checked with local HTTP fixtures. The LM Studio trial below covers only the named local configurations, not all architectures or server versions.
- Improved fiction quality, novel-scale continuity, lower author effort, accessibility, or crash-atomic multi-file transactions.

Those are trial results to collect, not claims inferred from passing software tests.

## API catalogs and inference settings

- Deterministic HTTP tests cover output-cap omission, explicit Chat Completions token fields, reasoning effort and chat-template controls, unsupported reasoning choices, optional usage counters, and reasoning state retained across tool messages.
- Application tests exercise authenticated catalog refreshes, additions/removals, offline cache retention, restart recovery, stale responses after connection edits, detached metadata views, and selection from an already-open form. Refreshing does not run inference or modify author configuration versions.
- Engine tests exercise a complete draft/review workflow with different inherited settings per stage and an unlimited operation deadline, checking the actual model/client arguments and retained trace options.
- Browser checks cover connection creation, model selection, reported reasoning choices, settings persistence, and assistant submission against a controlled HTTP API. Captured request parameters verify reasoning off and omitted token caps. Fixtures verify request construction and app behavior, not real-model writing quality or whether a third-party server honors a setting.


## Budget and compatibility gap fixes

- Automatic budgets account for tool schemas and expanding tool history within an 8,192-token loaded context. Tests cover stage allowances with reasoning off, Unlimited omission, overfull context rejection, and retained whitespace/filtered/malformed responses.
- Discovery fixtures cover LM Studio aliases with a 262,144-token model maximum but an 8,192-token loaded instance, Ollama capabilities and loaded context, and llama.cpp `/props`. Capabilities refresh on saved models while explicit author tool overrides survive. Unselected rows do not become authored configuration during unrelated saves.
- Service/API regressions cover changing a shared connection through its own command, saving model settings without rewriting credentials/URL, rejecting unsupported inherited reasoning, and rejecting null outline entries without panic. Multi-call tool probes now complete every valid tool result; they no longer falsely reject two valid calls.
- A real local LM Studio trial used the already-downloaded Qwen3 Coder 30B MLX 4-bit model, loaded with 8,192 context under `iterauthor-compat-test`. The browser Test model operation passed text and a complete tool exchange. A single-outline assistant request produced seven bullet points after a search tool and a second model call. The inspector showed output allowances of 6,382 and 6,205, estimated inputs of 786 and 963, and actual server inputs of 519 and 601. The reply is evidence of operation, not a judgment of fiction quality or factual accuracy.
- Browser checks inspected loaded-context labels, saved Unlimited, confirmed new-project Automatic, and read settings/usage per call. The original project tab and source files were not used for the trial. Test servers and loaded test models were stopped after verification.
- A second live LM Studio trial used the downloaded Qwen3.6 40B GGUF IQ2_M reasoning model at 8,192 context. The API advertised Off/On. A request with reasoning Off returned `Ready`, no reasoning text, and zero reported reasoning tokens. This establishes that the configured control worked for this instance; it does not verify every provider/template.


## Console activity and errors

Regression tests exercise plain generation stage messages, model discovery failures, rejected HTTP commands, token exhaustion, and provider authentication errors through the real application/HTTP layers. They assert ERROR severity and useful failure summaries while excluding echoed prompts, story text, credentials, arbitrary command values, and query strings. Repeated state polling produces no log output. Existing SSE tests cover streaming through the status-logging response wrapper. The HTTP executable installs logging before starting background model refresh; the optional TUI keeps its terminal surface free of console messages.
