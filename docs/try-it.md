# Trying Iterauthor

## Debian and direct browser access

Build with `make release`. Choose `iterauthor-linux-amd64` for `uname -m` = `x86_64`, or `iterauthor-linux-arm64` for `aarch64`.

```sh
# On your Mac; substitute your SSH destination.
scp dist/iterauthor-linux-amd64 you@debian:~/iterauthor
ssh you@debian

# On Debian, in that SSH session.
chmod +x ~/iterauthor
~/iterauthor --new --sample --demo --listen 0.0.0.0:8080 ~/iterauthor-trial
```

Open **http://YOUR-DEBIAN-HOST:8080** in your Mac's browser, using the server's hostname or IP. iTerm runs the SSH session; the browser handles editing, mouse navigation, and menus. No browser or frontend runtime is needed on Debian. HTTPS model endpoints require Debian's usual CA certificate store.

`--listen` accepts ordinary bind addresses: `0.0.0.0:8080`, `:8080`, `[::]:8080`, or a particular interface such as `192.168.1.10:8080`. Without the flag, the default remains `127.0.0.1:8080`. The server accepts browser requests addressed to its hostname or IP. This single-author test build has no authentication; anyone who can reach the listener can access the open project.

SSH forwarding is optional. To use it, keep the default localhost listener and connect with `ssh -L 8080:127.0.0.1:8080 you@debian`, then open **http://127.0.0.1:8080** on your Mac. If local port 8080 is occupied, use `-L 8081:127.0.0.1:8080` and open port 8081. The tunnel's destination port must match the server's listen port.

To keep the server running after an SSH disconnect, use an existing `tmux` session or your normal process supervisor. Closing a browser tab does not cancel generation. Stopping the server with Ctrl+C requests cancellation and waits briefly for checkpointing. Completed work is retained; queues do not automatically resume after restart.

Reopen for direct network access with `~/iterauthor --demo --listen 0.0.0.0:8080 ~/iterauthor-trial`. From inside the project directory, omit the path: `~/iterauthor --demo --listen 0.0.0.0:8080`. Plain `~/iterauthor` opens the current project using its configured real models. `~/iterauthor --new` creates a project in an empty current directory.

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

Open **Models → Add API connection**, enter a name and API URL, and save. A server address, compatible API base, or model-list URL is accepted. Choose a **Base model** from the connection's dropdown group. Other models from the same API are immediately available for task and outline assignments; you do not need a separate connection for each model.

Model catalogs refresh every minute and when you use **Refresh models**. Discovery reads compatible `/v1/models` lists plus optional LM Studio/Ollama metadata. A failed refresh preserves the last successful list and shows its error. Models disappearing from a catalog remain in saved assignments; the app never chooses a replacement for you.

Choose a connection's model and open **Model settings** for reasoning, output limits, and the optional **Test model**. Tests make at most four inference calls and have a five-minute deadline; they may load models or incur provider charges. The test can negotiate the output-token field and verify a full tool exchange. It is not required just to connect an API. For models with unknown tool capability, an explicit unverified label stays visible until testing.

Reasoning and output limits inherit from model settings through task defaults and outline ancestors. **Unlimited (no app cap)** omits the request's output-limit parameter; server limits still apply. New projects use it by default; old limits stay as saved. In **Project settings**, set minutes to zero to remove the operation time limit. Stop remains available, and model-call/draft limits still bound the loop.

Addresses are resolved **from Debian**, so its `127.0.0.1` refers to Debian. For a model server on your Mac or LAN, use an address reachable from Debian or another SSH tunnel. Common server addresses are `http://YOUR-MODEL-HOST:1234` for LM Studio, `http://YOUR-MODEL-HOST:11434` for Ollama, or the actual listen address of your llama.cpp server. Hosted providers require their compatible API base URL.

Use the optional API-key environment-variable field when authentication is required. Enter the name of an environment variable already set for the Iterauthor process, rather than the secret. The application does not read `.env` files or use ChatGPT account login. There is no silent fallback to demo output.

Start with one tool-capable base model. Add further connections and use **Change assignments** for task defaults. **Style → Change inheritance or model choices** overrides assignments for an outline and descendants. Selection, consistency, and interactive conversations require tool use; outlining, prose, and style accept text-only models.

Discovery references: [LM Studio model metadata](https://lmstudio.ai/docs/developer/rest/list), [Ollama model lists](https://docs.ollama.com/api/tags), and [llama.cpp server](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md). Compatibility is verified with local HTTP fixtures, not every live provider/model combination.

## External editing and recovery

While work is idle, edit Markdown or metadata from another shell, then select **Reload project files**. If work is active, use **Stop generation** and wait for it to finish before editing externally. Avoid simultaneous writes to the same source. Detected stale saves are rejected, but arbitrary external editors do not participate in an atomic transaction. Invalid metadata keeps the last valid in-memory configuration and reports an error.

Back up the whole project, including `.twriter`, to retain history, candidates, and conversations. Stop the server before inspecting the same project with another process:

```sh
~/iterauthor --check ~/my-novel
```

The optional legacy TUI runs with `--tui`. It retains mouse support and modifier-key commands, including Ctrl+G for commands, Ctrl+S for save, and Ctrl+R for assistant send. Its external-editor command uses `VISUAL`, `EDITOR`, or `vi`. The browser interface does not launch a remote editor process.
