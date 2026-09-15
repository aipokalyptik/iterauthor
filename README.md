# Iterauthor

A browser workspace for author-directed, model-assisted long-form fiction. This **0.2 test build** combines a world wiki, layered outlines, inherited writing guidance, and a bounded drafting/review process. Your project remains ordinary Markdown and JSON files.

Iterauthor combines *iterate* and *author*: develop a brief, generate a passage, inspect the result, and refine the sources that guide the next draft.

One Go binary serves the interface and runs the writing engine. Debian needs no Node runtime, frontend build, Python environment, or database. Connect from your browser directly or through an SSH tunnel.

```sh
./iterauthor --new --sample --demo ~/iterauthor-trial
```

Open **http://127.0.0.1:8080**. `--demo` uses explicitly marked synthetic replies and makes no model-server calls. It tests the workflow, not writing quality. Omit `--new` when reopening.

The project directory defaults to the **current working directory**. Run `iterauthor` inside an existing project, or `iterauthor --new` in an empty directory. Use `--listen 127.0.0.1:8081` if port 8080 is occupied. The previous terminal interface remains available with `--tui`.

To connect directly from another machine, run `iterauthor --listen 0.0.0.0:8080` and open `http://YOUR-SERVER:8080`. You can also bind to a specific interface address or use `--listen :8080` for all interfaces. Localhost remains the default.

## Connect an API

Open **Models → Add API connection**, name the connection, and enter its API URL and optional API-key environment variable. Save once, then choose a **Base model** from its model list. Each API/account has its own group in the model dropdowns for tasks, outlines, and conversations.

Lists refresh every minute, independently of generation, and have a manual **Refresh models** button. The last successful catalog is cached for offline use. Refreshing never switches a saved assignment or changes generation inputs. Existing per-model connections are grouped by API URL and credential reference while keeping their model IDs and assignments.

**Model settings** exposes reasoning and an inherited, automatic, unlimited, or custom output limit. Task settings and individual outlines can override both, with inheritance down the tree. Reported reasoning options constrain requests; unsupported settings produce an error instead of silently choosing another level. APIs without reasoning metadata use their default unless you explicitly choose an option. LM Studio loaded aliases, Ollama metadata, and llama.cpp context metadata are discovered automatically. A chat-template compatibility option is also available for servers that need it. Unknown tool support requires Test model or an explicit author override before research tasks can use that model.

New projects default to **Automatic** output budgeting. With reasoning off, it starts at 4,096 tokens for context selection/reviews, 8,192 for outlines/discussion/edits, and 16,384 for prose. With reasoning enabled or unknown it reserves up to 32,768, including hidden reasoning. Reported context capacity reduces the allowance to leave room for estimated input, tool schemas/history, and a 1,024-token margin. These are estimates, not exact tokenizer counts; source text is never silently removed. Explicit limits are also bounded by estimated context space.

**Unlimited (no app cap)** remains available and omits the output-limit parameter; the server's configured default can still limit output. Existing project limits are preserved, including previous Unlimited settings. Custom limits accept 64–1,048,576 tokens; 64 is a validation floor for deliberate small tasks, not a usable default for fiction. **Project settings** also permits a zero-minute time limit (unlimited); otherwise the operation's time limit controls requests, without a hidden five-minute HTTP cutoff. Model-call and draft-attempt limits remain separate. Unlimited does not automatically continue truncated output or retry calls.

The optional **Test model** checks text and a complete tool exchange, using up to four calls and five minutes. It can load a local model or incur provider charges. Tool capability that was not reported is labeled unverified; explicitly text-only models cannot serve tasks requiring tools. A tool-capable base model handles every task. Text-only models can serve outlining, prose, and style.

Inference uses compatible Chat Completions APIs. Discovery reads compatible model lists and optional LM Studio/Ollama metadata. Authentication uses an environment-variable name per connection; secrets stay on the server. Server model-loading settings remain outside Iterauthor.

## Write and revise

- **Outline:** navigate the tree, edit a brief, draft a passage or branch, and develop outline proposals.
- **World wiki:** maintain characters, places, world facts, and arbitrary entry kinds.
- **Style / Context & instructions:** inspect inheritance, attach references, and add local guidance.
- **Private notes:** keep reference text that is excluded from model context and tools.
- **Writing assistant:** discuss or propose edits within a fixed scope, even while browsing elsewhere.
- **Activity:** inspect candidates, consistency/style findings, prompts, context, and tool calls; apply chosen proposals.
- **Manuscript:** read active passages in story order and export Markdown.

Text saves with **⌘S / Ctrl+S**; assistant messages send with **⌘Enter / Ctrl+Enter**. Changes to outlines, style, context, or active model selections offer choices about which existing prose to revisit. Setup and planning need no review when there are no passages or draft candidates. Connection details, unused models, execution limits, and automatic scheduling apply to future work without requiring regeneration. You can explicitly regenerate whenever you want to try new settings.

Unsaved text is guarded when navigating. One operation runs at a time; source editing unlocks after completion or cancellation. Closing a browser tab leaves work running in the server.

The assistant shows submission feedback, the current model/tool stage, elapsed time, and the planned output allowance. Inspect result shows applied reasoning, requested/effective output limits, estimated input, and actual usage reported by the server for every call. Completed failures stay visible, with inspection and retry controls. If a model exhausts its output limit before producing text, choose Automatic, increase **Output tokens**, reduce reasoning in **Model settings**, or change the server limit when using Unlimited, then retry. Empty/whitespace replies, blocked output, malformed tool calls, and detectable violations of reasoning Off remain visible failures with the response retained. Discussion sessions reply in the conversation; source-edit sessions stage changes for review and application. The HTTP server prints operation stages and outcomes to its console without printing prompts or story text.

## Build and verify

Source builds require Go 1.24 or newer. Packaged binaries do not require Go.

```sh
make test
make build
./dist/iterauthor --new --sample --demo ./work/trial
make release
```

`make release` produces Linux amd64, Linux arm64, and macOS arm64 binaries, documentation archives, and SHA-256 checksums in `dist/`. Browser assets are embedded in every binary.

- [Debian, network access, and first walkthrough](docs/try-it.md)
- [Implementation and current limits](docs/test-version.md)
- [Application and interface architecture](docs/architecture.md)
- [Verification evidence](docs/test-verification.md)
- [Earlier TUI design](docs/ui-design.md)

This is a single-author experiment. Dependency scheduling, automatic multi-level outline expansion, resumable queues, streaming, and native provider-specific inference APIs remain future work. Context summaries are inspectable history; this build does not reuse them to skip calls. Reviewer approval is a model judgment, not proof of novel-wide continuity.
