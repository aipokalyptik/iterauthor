# Terminal test-build verification

September 14, 2026. Development host: macOS arm64, Go 1.27.0. Source declares Go 1.24 as its minimum; the actual compiler used was 1.27.0.

## Automated checks

`make test` passed: 17 tests across four packages, including the race detector, followed by `go vet ./...`. Each package has a 60-second test timeout; UI waits and fake network startup have shorter bounds.

- File store: private-note exclusion, leaf-to-parent conversion, inherited settings/attachments, stale external saves, invalid tree reload, single-process locking, failed-open cleanup, protected authored prose, stale candidate rejection, and changes discovered after restart.
- HTTP adapter: endpoint/model/token-field contract, tool-message exchange, cancellation, credential redaction on errors, and preservation of token-truncated text.
- Generation engine: a local HTTP fixture performs selection tools, fails an initial consistency review, receives a revised candidate, and passes consistency/style. Other tests cover call exhaustion, unknown tools, out-of-scope/private-note edits, malformed verdicts, and cancellation with a completed draft retained. Fixtures do not call a hosted or local LLM.
- Terminal simulation at 120×40 and 80×24: real mouse events select sources and editor buttons; modifier keys save and find commands; compact navigation generates a passage and persists its operation. A scoped edit conversation remains attached to its original source while browsing changes, and Apply changes only that source. Cancellation clears queued work and releases editing only after the active call stops.

The interaction tests exposed and now cover container replacement during TreeView drawing, rapid clicks on different buttons being misclassified as double clicks, and missing keyboard forwarding through dialog overlays. Long help content uses a scrollable view.

## Release checks

Release artifacts built successfully with `CGO_ENABLED=0` and `-trimpath`. The `file` utility identifies the Linux outputs as statically linked x86-64 and aarch64 ELF executables; the native output is macOS arm64 Mach-O. Archive contents include the executable, documentation, and dependency notices. Native `--new --sample --check`, reopening with `--check`, and `--version` passed. See `dist/SHA256SUMS` for artifact hashes. Linux binaries were cross-compiled, not executed on Debian here.

`python3 scripts/smoke-pty.py dist/iterauthor` passed against the compiled native app through a Unix pseudo-terminal with `TERM=xterm-256color`. It used actual SGR mouse reports to select a scene, open its editor, and place the caret inside the text; typed content was saved to the expected file. Command search and Help used modifier keys, and Ctrl+C exited successfully. The harness drains terminal output through exit and uses bounded waits. Python is a development-test dependency only.

## Not established by these checks

- Actual iTerm → SSH → Debian operation, SSH tunnel configuration, or behavior of a specific tmux setup.
- Live LM Studio, Ollama, llama.cpp, or hosted model quality/compatibility. No endpoint or credentials were supplied for those trials. Use Test base tools and a small passage first.
- Improved fiction quality, novel-scale continuity, lower author effort, accessibility, or crash-atomic multi-file transactions.

Those are trial results to collect, not claims inferred from passing software tests.
