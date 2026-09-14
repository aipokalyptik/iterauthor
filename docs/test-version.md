# Test-version implementation

This build concentrates on a usable end-to-end loop: author sources → select context/models → generate → review → inspect → revise sources → regenerate. It is not the complete scheduler in the longer-term UI design.

The [application service](architecture.md) owns workflow state, worker execution and persistence. The default HTTP/browser interface and optional TUI submit commands and render detached state through the same core. Browser assets are embedded in the Go binary.

## Files

```text
project.json                         # schema, ordered tree, wiki index, models, settings
outline/<id>/outline.md              # authored brief, any internal detail
outline/<id>/style.md                # inherited addition or replacement
outline/<id>/notes.md                # private author reference
outline/<id>/prose.md                # active text for a leaf
knowledge/<id>/entry.md              # wiki content
knowledge/<id>/notes.md              # private author reference
.twriter/state.json                  # passage state, changes, decisions, last model
.twriter/inputs.json                 # file hashes and generation fingerprint metadata
.twriter/project.lock                # single-process lock
.twriter/history/<version>/*         # previous authored text
.twriter/runs/<run-id>.json           # candidates, reviews, prompts, tool calls
.twriter/conversations/<id>.json      # scoped turns and draft input
exports/manuscript.md                # draft with visible gaps/status markers
exports/manifest.json                # source hashes and passage status
```

Markdown is authoritative text; `project.json` is authoritative structure. Stable IDs survive renames; child-array order determines manuscript order. Optional text files may be absent. Both interfaces add children/wiki entries; structural move/reorder/delete currently require paused external metadata editing and Reload.

The metadata directory retains its original `.twriter` name for compatibility with the first trial. Existing trial projects open directly in Iterauthor with their conversations, candidates, and history intact; no migration is required.

Writes use a temporary file, sync, and rename. Source saves retain previous content. A process lock excludes another iterauthor instance. Workers receive frozen snapshots; completion checks the generation fingerprint before automatically activating prose. Raw file hashes detect external edits separately. Generation fingerprints exclude operational settings and remain project-wide, so an unrelated writing-input change can hold a candidate for review. Unchanged projects retain their existing cached-run fingerprints when opened by this build.

Multi-file operations retain recovery history and commit structural metadata last; they are not a transactional filesystem. Backups have an inspection UI; restoration is manual. Keep `.twriter` even when invalidating cached results.

## Generation

Each leaf is one generation unit and can describe several scenes or a detailed internal outline. Ancestor outlines, effective style, and mandatory attachments are included. Automatic wiki/outline selectors can read/search source entries and supply summaries; both inherit independently and default on. Manual attachments remain mandatory with automatic selection off.

All ancestor outline text is included conservatively. There is no separate pruning model for ancestor paragraphs. Excessive required context produces a budget error rather than silently dropping requirements.

The writer produces a candidate, consistency reviews it using source tools, and style reviews it. Blocking findings go back to the writer. Each revision starts review again. Invalid/contradictory verdicts, missing explanations, truncation, timeouts, and exhausted budgets stop automatic acceptance. Preserved candidates can be explicitly chosen with findings.

Defaults: three drafts per leaf, 24 total model calls per queue, 2,500 output tokens per call, 60,000 input characters per call, and 20 minutes per operation/queue. Selection, tool continuations, and revisions all count. Tool output is bounded; each response permits at most eight tool calls. Input budgeting counts serialized message characters, not exact tokenizer tokens. There is no pricing estimate or dollar cap.

Passing prose may replace prior generated prose. Manual prose requires explicit candidate selection. Invalidation preserves prior text and records. Working-manuscript exports mark incomplete or retained material.

## Editing and assistant

Projects start paused. Save keeps them paused; Finish editing requires decisions on pending writing changes before releasing the pause. A setting generates eligible prose after editing is finished, rather than on each keystroke/save.

No prose review is requested until a leaf has active text or a saved, non-invalidated prose candidate. Setup and planning edits are recorded in history with the automatic disposition `no-prose`. Opening or reloading a project also resolves obsolete pending prompts when there is no prose to revisit. Outline proposals, connection tests, and other non-prose results do not create that requirement.

With prose present, edits to outlines, wiki facts, style, context attachments/selection, prompts, and effective model assignments still request a decision. Connection URLs, credential environment names, display labels, protocol capabilities, unused model entries, execution limits, and automatic scheduling are recorded as `future-only`: they apply to future work without blocking drafting or making cached candidates/proposals stale. Explicitly setting an already inherited model or automatic-selection value also leaves prose current. Changing an assigned model's actual model identifier does request review. Identical saves create no decision. Authors can explicitly regenerate to try operational settings; changing a URL to a different backend does not itself request regeneration.

One active operation locks source editing globally. Cancel clears the remaining queue and waits for the active call. Then edit and explicitly generate again. Subtree locks, cancel-on-edit for queued branches, and automatic re-queueing are not implemented.

Conversations retain target, mode, model, turns, and draft. Browsing does not retarget them. Later turns receive current sources plus saved discussion. Interactive research and source editing require a tool-capable model; text-only models remain usable for prose, outline generation, and style review. Demo editing supplies a synthetic replacement. Apply makes it authored source and refreshes views. Stale proposals are rejected when fingerprints differ, and the entire set of proposals is validated for scope and permitted fields before writes start.

Private notes are excluded from worker snapshots. Models have no arbitrary filesystem, shell, web-research, or history-read tools. The exposed tools are `read_entry`, `search_entries`, and scoped `propose_edit`. Diagnosis cannot yet query older run records through a tool; authors can inspect and quote that evidence.

## Limits to assess

- Generated outline detail is a textual proposal for one brief; import appends authored text. No generated subtree overlay or automatic expansion to a chosen depth.
- Summaries and outlines are cached for inspection/invalidation but not reused to skip future calls. Prose invalidation and generated-run invalidation are separate commands.
- No full reverse dependency graph. “Linked passages” uses explicit inherited attachments. Automatic reads are inspectable in traces but do not drive that selection; use selected passages or whole-story invalidation for wider effects.
- Leaf-to-parent conversion retains prose as an outline reference, a private note, or discards its active contribution. Outline reference keeps the text for refinement; no LLM summary is implied. Parent prose stops contributing to the manuscript.
- Style supports inherited additions or replacement of the whole inherited style, without named-rule replacement. Operation prompts append down the tree.
- Queues do not resume after restart. Completed calls/candidates are checkpointed; a call killed before returning cannot be recovered.
- No streaming, native Anthropic/Responses inference adapters, web research, multi-process editing, or publication formatting. URL-driven discovery supports compatible model lists and optional LM Studio/Ollama metadata; server loading/reasoning settings are informational rather than editable. Compatible endpoints can differ in model behavior and options.
- Browser and terminal interaction checks cover the main workflow. Accessibility, long authoring sessions, and actual iTerm/SSH forwarding to Debian still need trials.

The experiment should measure whether an author can diagnose a poor passage, change a relevant source, and obtain a better draft without managing excessive configuration. That result should guide the next scheduler and interface work.
