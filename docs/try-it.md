# Trying Iterauthor

## Copy to Debian

Build with `make release` or use the prepared binaries in `dist/`. Choose `iterauthor-linux-amd64` for `uname -m` = `x86_64`, or `iterauthor-linux-arm64` for `aarch64`.

```sh
# On your Mac; substitute your SSH destination.
scp dist/iterauthor-linux-amd64 you@debian:~/iterauthor
ssh you@debian

# On Debian, inside your SSH session.
chmod +x ~/iterauthor
~/iterauthor --new --sample --demo ~/iterauthor-trial
```

The binary is compiled with CGO disabled. Debian needs a working UTF-8 terminal and, for HTTPS endpoints, its usual CA certificate store. An 80×24 terminal works; 120×40 is more comfortable. iTerm normally reports `TERM=xterm-256color`. If the app reports that the terminal is not cursor addressable, check `TERM`; `dumb` is unsuitable.

Mouse reporting must be enabled in iTerm and passed through SSH/tmux. Use the terminal's selection override for native text selection. iterauthor needs no function keys or extended keyboard protocol.

To survive disconnects, run inside an existing `tmux` installation:

```sh
tmux new -s iterauthor
~/iterauthor --demo ~/iterauthor-trial
```

Without tmux, disconnecting can terminate the process. Completed calls/candidates have checkpoints; interrupted jobs appear in Activity after reopening. Queues do not automatically resume.

## First walkthrough

1. Click **The kitchen scene**, then **Guidance** and **Context**. Inspect inherited style and references. **Notes** is explicitly private.
2. Open **Outline → Edit**, add a requirement, and save with **Ctrl+S**. Generation stays paused.
3. Click **Finish editing**. Review the change and choose Keep, branch, selected passages, or the entire story. There are bulk choices for multiple changes. Finish editing again once the choices are resolved.
4. Open **Prose → Generate** and choose a scope. Activity shows progress. Open the completed operation for candidates, reviews, and **Exact inputs/tools**.
5. Select a scene and **Discuss**. Choose advice or proposed edits. Enter adds a newline; **Ctrl+R** sends. Browsing does not change the conversation's scope. Inspect an edit's **CURRENT / PROPOSED** text in Activity before **Apply proposals**.
6. Use **Generate outline detail** through **Ctrl+G**, inspect the proposal, and import useful detail. Use **Add child outline** for structural children at any depth.
7. Open **Manuscript → Export manuscript**. The project receives `exports/manuscript.md` and its source/status manifest. Missing and invalidated passages are marked.

Reopen with `~/iterauthor --demo ~/iterauthor-trial`. Repeated demo passages are expected: the demo tests operation of the tool, not the fiction-writing hypothesis.

## Real models

Create a separate project or reopen the trial without `--demo`:

```sh
~/iterauthor --new --title 'My novel' ~/my-novel
```

In **Settings → Models**, edit **Base model**. Supply a label, API base URL, and the exact model identifier your server offers. Typical URLs:

| Service | API base URL |
| --- | --- |
| LM Studio, same machine | `http://127.0.0.1:1234/v1` |
| LM Studio, LAN | `http://YOUR-MODEL-HOST:1234/v1` |
| Ollama compatibility API | `http://YOUR-MODEL-HOST:11434/v1` |
| [llama.cpp server](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) compatibility API | `http://YOUR-MODEL-HOST:8080/v1` |
| OpenAI | `https://api.openai.com/v1` |

Addresses are resolved **from Debian**. Its `127.0.0.1` is not your Mac. Configure your server's listening address or an SSH tunnel accordingly.

Credentials are environment variables. Enter the **variable name**, not the secret, in the connection form. For example, if the Debian process has `OPENAI_API_KEY` in its environment, enter `OPENAI_API_KEY`. iterauthor does not read `.env` files or support ChatGPT account login. Authentication is optional for servers that do not require a key.

Choose `max_tokens` or `max_completion_tokens` according to the endpoint/model. No model-specific temperature or reasoning controls are sent. Requests are non-streaming; Activity shows the stage while waiting for a complete reply.

Save the connection, then use **Test base tools**. The Supports tools checkbox is only a declaration; the test requires an actual `read_entry` call and round trip. A provider error is shown directly. There is no fallback to demo replies after a failed request.

Add connections for other hosts/models. **Feature defaults** selects models for outlining, writing, selection, consistency, and style. **Guidance → Models/style mode** overrides them for a subtree. Consistency and automatic selection require tools; writer, outline expansion, and style review accept text-only models.

Protocol references: [OpenAI Chat Completions](https://developers.openai.com/api/reference/resources/chat), [LM Studio tools](https://lmstudio.ai/docs/developer/openai-compat/tools), and [Ollama compatibility](https://docs.ollama.com/api/openai-compatibility). Tests exercise requests against a local HTTP fixture. Individual server/model combinations still need their connection test.

## External editing

Begin editing, or cancel active work and wait. **Open in external editor** suspends the TUI and runs `VISUAL`, `EDITOR`, or `vi`, then reloads after a successful exit. Alternatively edit from another shell and use **Project → Reload**.

Avoid simultaneous external/TUI writes to the same source. The app rejects detected external changes before saving/generating, but file checks cannot make arbitrary writers participate in a transaction. Invalid metadata leaves the last valid in-memory configuration intact and reports the error. Notes do not invalidate generation.

Back up the entire project, including `.twriter`, to retain conversations/candidates/history. Check structure without launching the interface using:

```sh
~/iterauthor --check ~/my-novel
```

This check still acquires the single-process project lock; close the project's TUI first.
