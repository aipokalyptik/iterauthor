# Test-version implementation

This build concentrates on a usable end-to-end loop: author sources → select context/models → generate → review → inspect → revise sources → regenerate. It is not the complete scheduler in the longer-term UI design.

The [application service](architecture.md) owns workflow state, worker execution and persistence. The TUI submits commands and renders detached state. A future interface can use the same core; no web server is included yet.

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
.twriter/inputs.json                 # known source hashes
.twriter/project.lock                # single-process lock
.twriter/history/<version>/*         # previous authored text
.twriter/runs/<run-id>.json           # candidates, reviews, prompts, tool calls
.twriter/conversations/<id>.json      # scoped turns and draft input
exports/manuscript.md                # draft with visible gaps/status markers
exports/manifest.json                # source hashes and passage status
```

Markdown is authoritative text; `project.json` is authoritative structure. Stable IDs survive renames; child-array order determines manuscript order. Optional text files may be absent. The TUI adds children/wiki entries; structural move/reorder/delete currently require paused external metadata editing and Reload.

The metadata directory retains its original `.twriter` name for compatibility with the first trial. Existing trial projects open directly in Iterauthor with their conversations, candidates, and history intact; no migration is required.

Writes use a temporary file, sync, and rename. Source saves retain previous content. A process lock excludes another iterauthor instance. Workers receive frozen snapshots; completion checks the fingerprint before automatically activating prose. Fingerprints are project-wide, so an unrelated external source edit can hold a candidate for review.

Multi-file operations retain recovery history and commit structural metadata last; they are not a transactional filesystem. Backups have an inspection UI; restoration is manual. Keep `.twriter` even when invalidating cached results.

## Generation

Each leaf is one generation unit and can describe several scenes or a detailed internal outline. Ancestor outlines, effective style, and mandatory attachments are included. Automatic wiki/outline selectors can read/search source entries and supply summaries; both inherit independently and default on. Manual attachments remain mandatory with automatic selection off.

All ancestor outline text is included conservatively. There is no separate pruning model for ancestor paragraphs. Excessive required context produces a budget error rather than silently dropping requirements.

The writer produces a candidate, consistency reviews it using source tools, and style reviews it. Blocking findings go back to the writer. Each revision starts review again. Invalid/contradictory verdicts, missing explanations, truncation, timeouts, and exhausted budgets stop automatic acceptance. Preserved candidates can be explicitly chosen with findings.

Defaults: three drafts per leaf, 24 total model calls per queue, 2,500 output tokens per call, 60,000 input characters per call, and 20 minutes per operation/queue. Selection, tool continuations, and revisions all count. Tool output is bounded; each response permits at most eight tool calls. Input budgeting counts serialized message characters, not exact tokenizer tokens. There is no pricing estimate or dollar cap.

Passing prose may replace prior generated prose. Manual prose requires explicit candidate selection. Invalidation preserves prior text and records. Working-manuscript exports mark incomplete or retained material.

## Editing and assistant

Projects start paused. Save keeps them paused; Finish editing resolves each saved change before releasing the pause. A setting generates eligible prose after editing is finished, rather than on each keystroke/save.

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
- No streaming, native Anthropic/Responses adapters, automatic provider discovery, web research, multi-process editing, or publication formatting. Compatible endpoints can differ in model behavior and options.
- Simulated terminal checks cover 80×24 and 120×40. Smaller screens, accessibility, and actual iTerm-over-SSH behavior need trials.

The experiment should measure whether an author can diagnose a poor passage, change a relevant source, and obtain a better draft without managing excessive configuration. That result should guide the next scheduler and interface work.
