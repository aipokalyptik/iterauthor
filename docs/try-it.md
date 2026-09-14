# Trying Iterauthor

## Debian over SSH

Build with `make release`. Choose `iterauthor-linux-amd64` for `uname -m` = `x86_64`, or `iterauthor-linux-arm64` for `aarch64`.

```sh
# On your Mac; substitute your SSH destination.
scp dist/iterauthor-linux-amd64 you@debian:~/iterauthor
ssh -L 8080:127.0.0.1:8080 you@debian

# On Debian, in that SSH session.
chmod +x ~/iterauthor
~/iterauthor --new --sample --demo ~/iterauthor-trial
```

Open **http://127.0.0.1:8080** in your Mac's browser. iTerm carries the SSH session and tunnel; the browser handles editing, mouse navigation, and menus. No browser or frontend runtime is needed on Debian. HTTPS model endpoints require Debian's usual CA certificate store.

Iterauthor deliberately listens only on localhost. SSH forwarding provides remote access to this single-author server. There is no public listener or login system in this test build. If local port 8080 is occupied, use `ssh -L 8081:127.0.0.1:8080 you@debian` and open port 8081 on the Mac. Change the server port separately with `--listen 127.0.0.1:8082` when needed; your tunnel's destination port must match it.

To keep the server running after an SSH disconnect, use an existing `tmux` session or your normal process supervisor. Closing a browser tab does not cancel generation. Stopping the server with Ctrl+C requests cancellation and waits briefly for checkpointing. Completed work is retained; queues do not automatically resume after restart.

Reopen with `~/iterauthor --demo ~/iterauthor-trial`. From inside the project directory, `~/iterauthor --demo` is sufficient. Plain `~/iterauthor` opens the current project using its configured real models. `~/iterauthor --new` creates a project in an empty current directory.

## First walkthrough

1. Select **The kitchen scene**. Edit its **Outline** and save using the button or **⌘S / Ctrl+S**.
2. Select **Review changes**. Keep existing prose, revisit the branch, choose passages, or revisit the whole story. Each change receives its own decision; bulk choices are available.
3. Open **Style** to edit local guidance and inspect effective inherited style. **Context & instructions** controls automatic selection, required references, and stage instructions. **Private notes** is excluded from model access.
4. Select **Draft passage**, confirm the scope, and inspect the completed result in **Activity**. Review candidates, both reviewers' findings, and exact context/tool traces. Manual prose requires explicit candidate selection before replacement.
5. Select **Discuss this outline** and choose discussion or proposed edits. Type a message and use **Send** or **⌘Enter / Ctrl+Enter**. Browsing another entry leaves the conversation's original scope intact. Inspect **Current / Proposed** text before **Apply proposed edits**.
6. Select **Develop outline** to request detail within the brief; import useful output as authored text. **+ Add** creates structural children. Adding children to a passage with prose asks what should happen to its existing text.
7. Read **Manuscript** and **Export Markdown**. The browser downloads a copy; `exports/manuscript.md` and a source/status manifest are also written in the project. Missing or outdated passages remain visibly marked.

Demo results repeat intentionally. They demonstrate the workflow, not model quality.

## Real model setup

Reopen without `--demo`, or create a separate project:

```sh
~/iterauthor --new --title 'My novel' ~/my-novel
```

Open **Models → Connect a model**, enter the API URL, and select **Discover models**. A server address, compatible API base, or model-list URL is accepted. Select a returned model, inspect available metadata, and run **Test selected model**. Save after the test completes.

The adapter discovers compatible `/v1/models` lists. It also checks LM Studio's native model listings and Ollama's `/api/tags` when available. Reported fields can include context size, loaded state, quantization, tool training, and reasoning choices. Missing fields remain unknown. Reasoning choices are informational in this build; model loading and generation settings retain their server defaults.

The test makes at most four small inference calls: text, optional output-token parameter retry, a harmless tool call, and its continuation. This can load a local model and incurs ordinary inference charges for a paid endpoint. The app automatically selects `max_tokens` or `max_completion_tokens`; you do not configure that field manually. A successful text test with failed tool use permits writing-task assignments, but cannot become the base model.

Addresses are resolved **from Debian**, so its `127.0.0.1` refers to Debian. For a model server on your Mac or LAN, use an address reachable from Debian or another SSH tunnel. Common server addresses are `http://YOUR-MODEL-HOST:1234` for LM Studio, `http://YOUR-MODEL-HOST:11434` for Ollama, or the actual listen address of your llama.cpp server. Hosted providers require their compatible API base URL.

Expand **Authentication** only when required. Enter the name of an environment variable already set for the Iterauthor process, rather than the secret. The application does not read `.env` files or use ChatGPT account login. There is no silent fallback to demo output.

Start with one tool-capable base model. Add further connections and use **Change assignments** for task defaults. **Style → Change inheritance or model choices** overrides assignments for an outline and descendants. Selection, consistency, and interactive conversations require tool use; outlining, prose, and style accept text-only models.

Discovery references: [LM Studio model metadata](https://lmstudio.ai/docs/developer/rest/list), [Ollama model lists](https://docs.ollama.com/api/tags), and [llama.cpp server](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md). Compatibility is verified with local HTTP fixtures, not every live provider/model combination.

## External editing and recovery

While work is idle, edit Markdown or metadata from another shell, then select **Reload project files**. If work is active, use **Stop generation** and wait for it to finish before editing externally. Avoid simultaneous writes to the same source. Detected stale saves are rejected, but arbitrary external editors do not participate in an atomic transaction. Invalid metadata keeps the last valid in-memory configuration and reports an error.

Back up the whole project, including `.twriter`, to retain history, candidates, and conversations. Stop the server before inspecting the same project with another process:

```sh
~/iterauthor --check ~/my-novel
```

The optional legacy TUI runs with `--tui`. It retains mouse support and modifier-key commands, including Ctrl+G for commands, Ctrl+S for save, and Ctrl+R for assistant send. Its external-editor command uses `VISUAL`, `EDITOR`, or `vi`. The browser interface does not launch a remote editor process.
