# Iterauthor

A browser workspace for author-directed, model-assisted long-form fiction. This **0.2 test build** combines a world wiki, layered outlines, inherited writing guidance, and a bounded drafting/review process. Your project remains ordinary Markdown and JSON files.

Iterauthor combines *iterate* and *author*: develop a brief, generate a passage, inspect the result, and refine the sources that guide the next draft.

One Go binary serves the interface and runs the writing engine. Debian needs no Node runtime, frontend build, Python environment, or database. Use your Mac's browser through an SSH tunnel.

```sh
./iterauthor --new --sample --demo ~/iterauthor-trial
```

Open **http://127.0.0.1:8080**. `--demo` uses explicitly marked synthetic replies and makes no model-server calls. It tests the workflow, not writing quality. Omit `--new` when reopening.

The project directory defaults to the **current working directory**. Run `iterauthor` inside an existing project, or `iterauthor --new` in an empty directory. Use `--listen 127.0.0.1:8081` if port 8080 is occupied. The previous terminal interface remains available with `--tui`.

## Connect a model

Open **Models → Connect a model**:

1. Enter the API URL and select **Discover models**.
2. Choose from the returned models. Available metadata appears beside the choice.
3. **Test selected model**, then **Save connection**.

The connection test checks text and a complete tool exchange, and automatically chooses the supported output-token parameter. A tool-capable base model handles every task. Text-only models can serve outlining, prose, and style. Task defaults and subtree overrides remain optional.

Inference uses compatible Chat Completions APIs. Discovery reads compatible model lists and optional LM Studio/Ollama metadata. Providers expose different amounts of metadata; the UI does not invent missing capabilities. Server loading, temperature, and reasoning settings retain their defaults. Authentication uses an optional environment-variable name; secrets stay on the server.

## Write and revise

- **Outline:** navigate the tree, edit a brief, draft a passage or branch, and develop outline proposals.
- **World wiki:** maintain characters, places, world facts, and arbitrary entry kinds.
- **Style / Context & instructions:** inspect inheritance, attach references, and add local guidance.
- **Private notes:** keep reference text that is excluded from model context and tools.
- **Writing assistant:** discuss or propose edits within a fixed scope, even while browsing elsewhere.
- **Activity:** inspect candidates, consistency/style findings, prompts, context, and tool calls; apply chosen proposals.
- **Manuscript:** read active passages in story order and export Markdown.

Text saves with **⌘S / Ctrl+S**; assistant messages send with **⌘Enter / Ctrl+Enter**. Saved source changes offer explicit choices about which prose to revisit. Unsaved text is guarded when navigating. One operation runs at a time; source editing unlocks after completion or cancellation. Closing a browser tab leaves work running in the server.

## Build and verify

Source builds require Go 1.24 or newer. Packaged binaries do not require Go.

```sh
make test
make build
./dist/iterauthor --new --sample --demo ./work/trial
make release
```

`make release` produces Linux amd64, Linux arm64, and macOS arm64 binaries, documentation archives, and SHA-256 checksums in `dist/`. Browser assets are embedded in every binary.

- [Debian, SSH forwarding, and first walkthrough](docs/try-it.md)
- [Implementation and current limits](docs/test-version.md)
- [Application and interface architecture](docs/architecture.md)
- [Verification evidence](docs/test-verification.md)
- [Earlier TUI design](docs/ui-design.md)

This is a single-author experiment. Dependency scheduling, automatic multi-level outline expansion, resumable queues, streaming, and native provider-specific inference APIs remain future work. Context summaries are inspectable history; this build does not reuse them to skip calls. Reviewer approval is a model judgment, not proof of novel-wide continuity.
