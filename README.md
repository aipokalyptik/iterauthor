# Iterauthor

A terminal workspace for author-directed, model-assisted long-form fiction. This is a working **0.1 test build**, with a Go TUI and ordinary Markdown/JSON project files.

Iterauthor combines *iterate* and *author*: develop a brief, generate a passage, inspect the result, and refine the sources that guide the next draft.

Copy one binary to Debian and run it over SSH. There is no database, browser, Python environment, or Node runtime to install on the destination.

```sh
chmod +x ./iterauthor
./iterauthor --new --sample --demo ~/iterauthor-trial
```

`--demo` uses marked synthetic replies and makes **no network calls**. It exercises the workflow, but cannot evaluate writing quality. Reopen with `./iterauthor --demo ~/iterauthor-trial`; omit `--new` when reopening.

The project directory is optional and defaults to the current working directory. Run `iterauthor` from inside an existing project to open it, or `iterauthor --new` in an empty directory to create one. An explicit path opens a project elsewhere.

For real writing, omit `--demo`, then configure a connection under **Settings → Models**. The adapter uses OpenAI-compatible Chat Completions endpoints. The selected model must actually support tools for context selection and consistency review; **Test base tools** checks a real tool round trip.

## What you can try

- World wiki with arbitrary entry kinds and an outline tree of arbitrary depth.
- Inherited prose style, per-feature model defaults, and overrides at any outline item.
- Independently inherited automatic wiki/outline context selection, explicit attachments, and author instructions for each stage.
- Multiline editing, external editing while paused, explicit Reload, and private notes excluded from model access.
- Scoped conversations, remembered model selection, and source proposals you inspect and apply.
- Leaf prose generation, branch/story queues, bounded consistency/style revisions, and inspectable candidates and tool calls.
- Source backups, per-change invalidation choices, and working-manuscript export.

Each leaf is a complete drafting brief and may contain its own sub-outline. Outline generation proposes detail inside a selected brief; importing it makes it authored text. Adding structural depth is an author decision.

## Navigation

Click sections, tree rows, tabs, buttons, dropdowns, and editor positions. At 100 columns or wider, navigator and document appear together. Smaller terminals switch between them; the tested compact size is 80×24. A tall, wide terminal docks the assistant below the document.

| Key | Action |
| --- | --- |
| Ctrl+G | Search commands |
| Ctrl+T | Cycle navigator, document, assistant |
| Ctrl+O | Switch navigator/document |
| Ctrl+S | Save the text buffer |
| Ctrl+R | Send the assistant message |
| Tab / Shift+Tab | Move among visible controls |
| Escape | Close dialog / leave compact assistant |
| Ctrl+C | Clean exit with unsaved-text/cancellation handling |

Enter adds a newline in text areas and activates buttons/menus. There are no function-key bindings. Help is a visible button and searchable command.

## Build and verify

Source builds require Go 1.24 or newer. Packaged binaries do not require Go.

```sh
make test
make build
./dist/iterauthor --new --sample --demo ./work/trial
make release
```

`make release` produces Linux amd64, Linux arm64, and macOS arm64 binaries, documentation archives, and SHA-256 checksums in `dist/`.

- [Try it on Debian over SSH](docs/try-it.md)
- [Implementation and current limits](docs/test-version.md)
- [Application architecture and future interfaces](docs/architecture.md)
- [Verification evidence](docs/test-verification.md)
- [Longer-term UI design](docs/ui-design.md)
- [Earlier browser interaction study](docs/ui-verification.md)

The largest intentional limitation is coarse scheduling: one operation runs at a time, and source editing waits for cancellation/completion. A dependency scheduler, automatic multi-level expansion, resumable queues, streaming, and native provider-specific APIs remain future work. Recorded context summaries are inspectable history; this version does not automatically reuse them to skip computation. Passing reviewers is a model judgment, not proof of novel-wide continuity.

The terminal uses an interface-independent application service. That service owns editing rules, queues, generation, cancellation and persistence; a future web interface can use the same operations. Headless tests verify that work completes without a terminal or progress listener attached.
