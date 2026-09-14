# Iterauthor: first interface specification

Status: longer-term interface design, September 14, 2026. A runnable Go test build now exists. See [test-version.md](test-version.md) for its actual implementation and explicit limits; this design includes behavior beyond that build. The earlier browser study uses sample data only.

## 1. What the interface must make obvious

At any moment the author should be able to answer:

1. Which project and item am I looking at?
2. Which control will receive my typing?
3. Am I looking at authored material, a generated candidate, or a historical version?
4. Which item and operation will a command affect?
5. What is the assistant allowed to read and change in this conversation?
6. Is story generation running, paused for editing, waiting, or in need of attention?

The interface has three independent targets: **viewed item**, **keyboard focus**, and **assistant scope**. Selecting a tree row changes the viewed item. Clicking an input changes focus. Starting a conversation or deliberately changing its scope sets assistant scope. They never change one another implicitly.

## 2. Decisions carried forward

- Files are authoritative. The TUI is a view and operator of that project.
- The knowledge store is an open-ended wiki; suggested categories are optional.
- Outline depth expresses detail. A leaf can hold a complete sub-outline or direct drafting prompt. Internal bullets do not automatically become independent generation jobs.
- Authored outline instructions take precedence over generated expansions. Generated material is inspectable, importable, and invalidatable.
- Effective style and feature model assignments inherit down the actual branch. Automatic knowledge selection and automatic outline selection each independently inherit On/Off.
- Knowledge and outline attachments are required context. Optional selection can add context when enabled. Author-reference notes never enter model reads, search results, summaries, or generation prompts.
- A capable base model covers all roles. Consistency research requires tools; prose, prepared-input outline generation, and style review can use text-only models.
- Manual conversations default to the last compatible manually chosen model, then the base model. Their choices do not rewrite automated feature assignments.
- External editing occurs while the story pipeline is paused. LLM-assisted editing is available during that pause.
- Reload reconciles external edits. Successful application-executed model edits cause the same reconciliation automatically. Reload does not resume generation.
- Generate reuses valid results and fills missing/invalidated work. Regenerate deliberately requests replacement. Invalidate preserves inspectable history.
- Writer, consistency reviewer, and style reviewer participate in one bounded revision loop. Every changed candidate starts review again; both passes apply to the same candidate.

## 3. Provisional interaction choices

These fill UI gaps and must be evaluated with the author rather than silently treated as previously agreed requirements.

1. **Five project sections:** Outline, Knowledge, Manuscript, Activity, Settings. The assistant is a persistent companion panel with saved sessions, not a sixth competing project hierarchy.
2. **A two-pane workspace plus an optional assistant:** navigator at left, selected content at right, assistant below by default. Navigation keeps sufficient room for reading and writing.
3. **Direct edits to prose become authored/protected.** Regeneration creates a candidate for comparison. It cannot silently replace an authored passage.
4. **The assembled manuscript is a reading/export view initially.** Each passage links back to its editable source. This avoids an undefined merge between an edited export and leaf files.
5. **Explicit outline expansion only in the first prototype.** The author chooses whether to generate detail inside the current drafting brief or propose child outline items. The UI does not infer a universal expansion depth or an automatic readiness score.
6. **Saved changes are reviewed in one change-review surface.** Every meaningful change has an invalidation decision. The author can decide immediately or at Finish editing; intermediate saves never secretly resume the pipeline.
7. **The initial internal editor handles plain text fields and drafts.** External-editor access is a peer action. Advanced selection, rich formatting, and structural drag-and-drop are deferred.

## 4. Layout

### Normal workspace: approximately 110–140 columns, 32 or more rows

```text
Project v    View v    Help v    Commands...    The Long Return       EDITING
Outline | Knowledge | Manuscript | Activity 2 | Settings      Finish editing
-------------------------------------------------------------------------
Outline                     | Chapter 3 / The visit      AUTHORED  Actions v
Find item...                | Outline | Prose | Guidance | Context | History | Notes
v Chapter 1                 |--------------------------------------------------
  Arrival                   | Mara visits her brother about their mother's
  The letter                | letters. He denies having received them.
> Chapter 2                 |
v Chapter 3                 | - She recognizes the cup.
  The visit          ready  | - She conceals her reaction.
  Departure         missing |                          Edit       Discuss
-------------------------------------------------------------------------
Assistant: Outline conversation 1  v      Scope: The visit v     Model: Base v
[conversation and changes; independently scrollable]
Prompt: [                                                        ]  Send
-------------------------------------------------------------------------
Viewed: The visit       Focus: prompt       1 saved change       Pipeline paused
```

The mockup is a spatial sketch; terminal cell counts, exact type sizes, and color mapping require a real terminal prototype. No hover-only controls are required to operate the application.

### Stable regions

| Region | Responsibility | Remains stable when |
|---|---|---|
| Project/menu row | Project operations, global command access, current mode | An item or section changes |
| Section row | Select a project section | The inspector tab changes |
| Navigator | Find/select items within the current section | The assistant receives text |
| Inspector header | Current item, provenance/status, item Actions | A local inspector tab changes |
| Inspector body | Read or edit the selected aspect | Another panel scrolls |
| Assistant header | Session, scope, role, model, permitted actions | The author browses another item |
| Status row | Focus, saving, pending changes, work state | Background jobs complete |

The selected item's main action is visible in its content view: Edit for authored text, Inspect/Import for a generated outline, Review for a passage requiring attention. The Actions dropdown contains the rest. The primary action and menu entry invoke the same command.

### Compact layouts

- At 80–109 columns, the navigator can be toggled with View → Navigator. The selected path remains visible. The assistant occupies the work area when expanded; an explicit Back to document returns to the previous location.
- Below 80 columns or 24 rows, use one panel at a time. Sections, current item, mode, and View remain available. Do not shrink the text or silently remove commands.
- The prototype includes a narrow layout study. It is not evidence of compatibility with a particular terminal.
- Resize preserves selection, buffers, scope, and focus where the control remains visible. If a focused panel is hidden, move focus to the control that restores it and retain its former caret position.

## 5. Mouse and keyboard contract

| Gesture | Result |
|---|---|
| Click section name | Switch section; restore its previous selection/tab/scroll |
| Click item row | Select that item and show it; do not edit or change assistant scope |
| Click disclosure marker | Expand/collapse that branch; keep selection unless its selected descendant becomes hidden |
| Click editable text | Focus the editor and position its insertion cursor |
| Click a labeled button | Execute its named command against its displayed target |
| Click menu trigger | Open that menu; only one command menu is open at once |
| Click outside a menu | Dismiss it without changing the command target |
| Wheel/trackpad over pane | Scroll that pane without moving the insertion cursor or stealing focus |
| Tab / Shift+Tab | Move through visible interactive controls in visual order |
| Arrow keys in navigator | Move selection; Left collapses/goes to parent, Right expands/goes to child |
| Enter on navigator row | Focus/open its content; it is not a destructive action |
| Arrow keys in menu | Move among commands; Enter executes |
| Escape | Close the top menu/dialog; otherwise leave the focused transient mode |
| Ctrl+G | Open the command chooser with the current target shown |
| Ctrl+T | Cycle navigator, inspector, assistant panes |
| Ctrl+O | Switch navigator/document in compact layouts |
| Ctrl+C | Clean exit, handling unsaved buffers and active work |
| Ctrl+S in editor | Save this buffer, without finishing the project editing session |
| Ctrl+R in prompt | Send the prompt; Enter inserts a newline |

Use modifier keys; do not assign function keys. Help has a visible button and a command-chooser entry. All actions have visible mouse-accessible paths. Plain letter shortcuts do not execute while an input has focus. Escape never discards an unsaved buffer without a decision. Menu/dialog dismissal returns focus to the invoking control.

Clicking a field or typing does not move a source item, start generation, or retarget a conversation. Disabled commands remain visible with a short reason. A small state explanation is available on keyboard focus/click; essential information does not rely on a hover tooltip.

## 6. Project sections and local inspectors

### Outline

The navigator shows the authored hierarchy. Generated proposals are visibly marked and can be shown or hidden through View → Generated outline detail. They never masquerade as authored entries. The selected root represents story-wide guidance.

| Inspector | Contents and actions |
|---|---|
| Outline | Leaf drafting brief or parent intent; Edit, Open in editor, Discuss, Generate detail |
| Prose | Active passage for a leaf; ordered read-only subtree assembly for a parent; prior/candidate status; Edit source, Generate, Review candidate |
| Guidance | Style, operation prompts, model assignments; each row shows local setting, effective value, and originating ancestor |
| Context | Two inherited automatic-selection controls; required attachments; last actual context used; source links and versions |
| History | Saved source changes, generated candidates, reviews, imports, and invalidation decisions; inspect/compare/use version |
| Notes | Private reference text; explicitly labeled Excluded from model context |

Guidance uses one common inheritance control everywhere: **Inherit / Set here**. Boolean features use **Inherit / On / Off**. The row always explains the effective result, such as “On — inherited from Chapter 3.” Removing a local model override means selecting Inherit, not copying today's inherited model into the field.

Style amendments and replacements are separate fields/actions. A replacement identifies which inherited instruction it replaces. If an instruction has no precise replacement target, it is an amendment or a source conflict requiring attention; it is not silently treated as a complete replacement of the guide.

Operation prompt adjustments have a clearly labeled operation selector. Changing the selector changes which operation is being edited, not the whole item. Model rows use the same role labels as Settings and Activity: Knowledge selection, Outline selection, Outline expansion, Prose writing/revision, Consistency review, Style review. Manual assistance uses its session selector.

Required attachments show where they were attached and whether they include the referenced item's descendants. Proposed default: an attachment applies to its owning outline and descendants. References inherited from an ancestor are labeled with their origin; editing their scope opens that originating attachment. They never bring the referenced outline's model or style overrides into the current branch. The context inspector distinguishes effective settings for the next run from the immutable input record of an earlier run.

### Knowledge

The navigator offers a searchable list and optional author-defined groups. No fixed character/place/world taxonomy is required. The inspector has Entry, Links, History, Notes. An entry may be linked from multiple outlines without being copied. A Links view lists consumers and lets the author jump to them.

### Manuscript

Read the assembled active passages in story order. A margin/header identifies each contributing leaf and its status; an unobtrusive “Open source” action returns to its Prose tab. Missing, invalidated, or unresolved passages are explicitly represented as gaps/status markers in the working preview.

Exports include a version manifest. If unresolved or missing passages exist, the export dialog lists them and offers an explicitly labeled working-draft export; it must not silently present incomplete output as complete. Formatting for publication is not part of the first UI experiment.

### Activity

List actual work with item, role, model/connection, state, elapsed time, and attempts/budget used. Select a row to inspect its input snapshot, current result, findings, and stop reason. Offer Cancel pending work, Cancel running work, Retry, Open item, Inspect inputs. Cancellation and retry are distinct actions.

The author can filter to Needs attention or Active work. The root summary is derived from jobs and author decisions, not a single model-authored progress narrative. A cost display distinguishes reported cost, estimate, and unavailable pricing.

### Settings

Navigation groups: Project, Models, Generation, Editing/interface.

- Project: project name, file location, export preferences.
- Models: configured connections/models and capabilities; base model; feature defaults. Model capability applies to a model-and-connection pair. Test connection and Test tool use are separate results.
- Generation: On command / After saved changes; limits for one passage and the overall run; review policy; model role links.
- Editing/interface: external editor command, layout preferences, keyboard help, text wrapping.

Story-wide style and outline intent remain at the root outline item. Settings provides an “Open story guidance” link instead of a second editable copy.

## 7. Command locations and scope

There is one internal command definition for each operation. Menus, primary buttons, the command chooser, and keyboard shortcuts use that definition. A command records its target when opened; browsing elsewhere cannot change an already-open action.

| Command | Canonical visible access | Scope / important behavior |
|---|---|---|
| New/Open/Recent project | Project menu | Project; resolve unsaved work before switching |
| Reload external changes | Project → Reload | Project; reconcile, preserve pause, report errors |
| Open project folder | Project menu | Current project directory |
| Export manuscript | Project menu or Manuscript actions | Selected branch or whole manuscript; disclose gaps |
| Edit | Selected content primary button / Actions | This source buffer; enters editing safely |
| Open in external editor | Actions → Edit externally | Target file; generation already paused; reload on return |
| New item / Add child | Navigator toolbar / Actions | Explicit parent shown before save |
| Rename | Actions → Rename | Identity remains stable |
| Move / Reorder | Actions → Move | Destination and order dialog; editing required |
| Delete item | Actions → Delete | Show subtree and references affected; explicit destructive confirmation |
| Generate outline detail | Actions | Brief or child proposal; no inferred global depth |
| Import generated outline | Generated item's primary action / Actions | Selected generated item/subtree; author ownership begins |
| Discuss | Primary button / Actions | Opens/resumes a session with explicit scope; no automatic scope change |
| Attach context | Context → Add attachment | Knowledge or outline picker; selected item/subtree inclusion shown |
| Change guidance/model | Guidance | Local overrides and descendants, unless explicit narrower scope |
| Generate | Prose action / Commands | Missing or invalidated prose in chosen scope |
| Regenerate | Prose Actions / Commands | New candidates for all eligible passages in chosen scope |
| Invalidate | Prose Actions / Commands | Chosen artifact kinds and scope; retained history |
| View history / Compare | Inspector History | One source item or candidate pair |
| Use candidate/version | History or candidate review | Explicit active-version change; preserves other candidates |
| Pause to edit | Project mode control | Stop scheduling; cancel or finish active work before entering Editing |
| Finish editing | Project mode control | Reconcile changes, resolve invalidation choices, release pause |
| Queue/resume requested work | Activity / mode control | Honor previously requested work and selected generation setting |
| Toggle assistant/navigator | View menu | Presentation only |
| Help / Diagnose current item | Help menu / assistant | Read-only help; diagnosis is evidence plus hypotheses |

### One scope chooser

Generation and cache operations use the same scope vocabulary:

- **This passage:** only meaningful for a prose-generating leaf.
- **This branch:** all contributing leaves under the displayed item, including itself if it is a leaf.
- **Selected passages:** explicit selection across branches.
- **Entire story:** all contributing leaves.

The confirmation/review surface shows the actual item/path, count, artifact kind, and work estimate when available. No command relies on “here” or “below” without displaying a target. Selecting Entire story is never inferred from clicking a root-level blank area.

Generate and Regenerate are separate menu entries. Both use an action review with target and budget, not an additional generic “Are you sure?” box. Invalidation without generation is explicitly available. Cache commands name the artifact kind: generated outline, prepared context, or prose. Invalidating an outline shows dependent generated artifacts affected and preserves authored/imported content.

## 8. Editing and change review

### Entering editing

From Ready, Edit begins editing immediately. From Generating, Edit displays “Pause generation to edit [target]” and offers **Finish active jobs, then edit**, **Cancel active jobs and edit**, and **Back**. The default does not interrupt a job without an explicit choice. Pausing stops new jobs from starting. Late results from canceled/superseded work cannot become active.

During Editing, the author may navigate, use read-only help, edit text, and instruct an editing assistant. The story generation pipeline remains paused until Finish editing. The initial operating contract is one active writer on a given file at a time; a paused pipeline does not make simultaneous external human and assistant writes safe.

### Internal editor

Edit opens an explicit buffer with Save and Cancel edit. Click places the caret. Ctrl+S saves; it does not resume generation. Navigation while dirty opens a single decision: Save and continue / Discard buffer / Keep editing. Save validates applicable structure before accepting the buffer. Failed validation retains the author's text and shows a specific error.

### External editor and Reload

Open in editor enters/uses Editing. On return, Reload imports changes; integrated tool edits invoke the same reconciliation automatically. If an internal buffer is dirty, Reload first resolves it. Reload preserves selection and assistant scope by stable ID when possible. If the selected item was deleted, select its surviving ancestor and explicitly explain the move. Invalid references are shown with repair links. Reload does not regenerate or discard content.

### Invalidation choices

Each saved source change is recorded. Changes shows an unresolved decision for each meaningful change: **Keep existing prose**, **Selected passages**, **This branch**, or **Entire story**. The author may resolve decisions while editing or in one consolidated Finish editing surface. Selecting a scope for one change does not silently apply it to the others; an explicit Apply to all control can do that.

The full chooser also allows additional selected passages alongside a branch, with the combined count shown. For a knowledge entry, use **Linked passages** instead of the meaningless label This branch. Show known consumers and label how those links were discovered; this is not a claim to have found every narrative consequence. Selected passages and Entire story remain available. In the interaction study, linked consumers are limited to explicit attachments.

Finish editing is unavailable until structural errors and unresolved invalidation choices are addressed. The author can always return to editing. The review shows the union of affected passages and treats overlaps once. Keep records intentional retention after a source change; it does not falsely label the prior generation as having consumed current inputs.

With On command, completing editing leaves unrequested new work waiting. Previously requested suspended work may resume, and the review says so. With After saved changes, finishing the editing session queues eligible changed/missing work. A deliberately retained valid passage is not regenerated merely because the setting is enabled.

### Turning a leaf into a parent

Adding the first child to a leaf containing prose requires one decision:

1. **Convert prose to an outline**: create editable outline material, preserve existing instructions, and expose conflicts. A model-backed conversion is an editing task with its own cancel/error state.
2. **Keep prose as a private note**: save the exact text in Notes, excluded from LLM inputs.
3. **Discard the prose**: remove it without conversion.

Back cancels the child creation. No branch conversion is committed while its required model operation is incomplete. When completed, the parent ceases to contribute a separate passage to the manuscript. Child prose supplies its coverage. The author can undo the whole structural operation through source history before subsequent incompatible edits.

## 9. Assistant sessions

The author chooses Discuss on an item or opens Assistant from View. A new-session surface shows:

- Activity: Organize my ideas, Develop an outline, Revise text, Research continuity, Diagnose, or Ask for advice.
- Write scope: selected item, selected subtree, or read-only.
- Read context: supplied references and permitted research scope; private Notes excluded.
- Model: last compatible manual selection, then base; incompatible models explain the missing capability.

The activity sets initial instructions and permitted actions, not an immutable personality. The author can adjust the instructions. “Organize my ideas” captures and organizes supplied ideas; it does not independently invent plot developments without direction.

The session header remains visible and names activity, exact write scope, model/connection, and read-only versus editing status. An explicit Scope control changes it. Browsing never retargets it. A Scope action validates that new permissions and model capabilities fit the activity.

Prompt input is multiline: Enter adds a line; Send/Ctrl+R submits. While an editing turn is applying changes, the same files cannot be edited through the TUI. Cancel stops further assistant actions before handing control back. Incoming replies do not steal focus, move the viewed item, or force-scroll an author reading earlier messages; show New messages instead.

Each reply includes a compact changed-item list with View changes and Open item. This is grounded in actual saved results, not the model's description alone. A text-only editing task can return a replacement for a predetermined target; an exploratory research/editing task requires appropriate tools. Neither type receives private reference notes. Conversation transcripts and dictated working material are model-visible session material, explicitly distinct from Notes.

Before a subsequent assistant turn, refresh the relevant source versions. If manual edits occurred, send the updated files instead of replaying obsolete content as current. A changed model starts subsequent work with the current session brief and authoritative files; it is not silently given unrelated conversations.

## 10. Review and failure surfaces

Use explicit statuses with words as well as color:

| Status | What it means | Next useful action |
|---|---|---|
| Missing | No current passage exists | Generate |
| Available | A usable generated version exists | Read / inspect inputs |
| Authored | Author-controlled text is active | Edit / generate candidate |
| Retained after changes | Author chose to keep older prose | Read changes / regenerate |
| Invalidated | Prior result exists in history, but is not eligible for reuse | Generate / inspect old version |
| Queued | Work requested but not started | Inspect / cancel pending |
| Running: [role] | Named operation is active | Inspect / cancel |
| Paused for editing | Pending work suspended by author editing | Finish editing |
| Needs review | Budget exhausted, source conflict, or unresolved findings | Review candidates / edit instructions |
| Kept with findings | Author selected a candidate with unresolved review findings | Read findings / revise |
| Failed | An operation could not complete | Inspect error / retry |

The passage review shows candidate identity, exact input snapshot, consistency findings with source links, style findings with instruction links, optional suggestions, call/time/cost accounting, and stop reason. It offers Keep this candidate, Return to editing, and Continue with a new explicit budget. Continuing a failed/limited run never receives an unlimited budget.

No review badge claims novel-wide correctness. Passing means the configured checks found no blocking violations in that specific version. A candidate produced after the last reviews is visibly unreviewed.

Prose regeneration produces candidates without overwriting active authored prose. Using a generated candidate is an explicit choice when authored prose is active. Exact protected-text editing semantics are a prototype proposal to validate with the author.

## 11. First launch

1. Project → New project: choose name and directory.
2. Configure one base model connection and test text and tool capability. Credentials use the application's credential facility, not authored story files or generation logs.
3. Show the root outline in Editing with two initial tasks: Write story outline and Write style guide. The wiki can begin empty.
4. Offer Edit or Discuss for the outline. No requirement to choose all feature models first.
5. Finish editing → choose intended generation action and limits. Do not generate on first launch simply because a project was opened.

Opening an existing project validates it, restores navigation and saved sessions, and shows outstanding work. It does not silently restart interrupted model calls. The author can review and resume them.

## 12. Walkthroughs to exercise

| Journey | Required path | Failure to look for |
|---|---|---|
| Start with an idea | New → base model → root Outline → Discuss | Forced setup of every feature |
| Dictate successive outline points | Discuss → Organize my ideas → several prompts → inspect changes | New prompt uses stale files or invents beyond task |
| Mix manual and assisted editing | Edit externally → Reload → continue session | Assistant overwrites manual changes |
| Inspect a character during a scene conversation | Knowledge → entry → return to Outline | Conversation scope silently follows navigation |
| Change local pace | Outline → Guidance → Style amendment/override | Global guide accidentally replaced |
| Change one feature's model | Guidance → role → Set here | Other role assignments unexpectedly change |
| Pin a continuity reference | Context → Add → Knowledge/Outline → inspect effective context | Automatic selection drops the required attachment |
| Disable automatic knowledge only | Context → Knowledge Off; Outline Inherit | One switch disables both operations |
| Edit a vase color | Edit → Save → change review → chosen passages → Finish | Whole story invalidated without author choice |
| Change central motivation | Edit entry → affected links → Entire story decision | Cross-branch effects impossible to reach |
| Turn a leaf into a branch | Add child → conversion choice → save | Parent and child prose both render |
| Inspect/import generated outline | Show generated detail → inspect → Import | Imported content remains disposable cache |
| Reviewer reaches its budget | Activity → Needs review → candidate/findings | Work disappears or is falsely marked passed |
| Preserve an authored paragraph | Edit prose → save → regenerate candidate → compare | Authored text overwritten automatically |
| Choose a text-only specialist | Settings/Guidance → compatible role | Model offered for tool-required consistency research |
| Browse with mouse | Sections → item → tab → menu → action | Wrong target, focus loss, hover-only command |
| Use keyboard while writing | Tab/Ctrl+T/menu → return to prompt → Enter | Letters execute commands; newline sends prematurely |
| Resize with a draft open | Compact view → restore workspace | Lost buffer, hidden scope, clipped controls |
| Read manuscript | Manuscript → open source → edit leaf | No clear way back to the contributing text |
| Recover interrupted work | Open project → Activity → inspect/resume | Old jobs publish into an edited project |

## 13. Prototype boundaries and evaluation

The interaction study should demonstrate navigation, menus, caret/focus, scoped conversation, local model inheritance, separate context controls, edit/save/change review, generation states, and the leaf-to-parent decision. It uses deterministic fixture changes so the UI can be examined without inference costs. It is not an implementation of file persistence, model execution, terminal mouse protocols, or a complete editor.

Verify meaningful mouse journeys, keyboard operation, retained scope/focus, narrow layouts, and both appearances. A real TUI trial must additionally validate terminal key delivery, mouse reporting, wrapping/caret geometry, wide characters, paste handling, editor handoff, and restoration on exit.

Evaluate author effort: number of times they lose their place, choose a wrong scope, encounter a surprising write/run, or have to search for a command. The UI succeeds when an author can explain what happened and correct it without reconstructing the program's internal machinery.

The highest-risk interaction is a long dictation session producing a long change-review list. Exercise it with twenty successive prompts, then a manual edit, then a model-assisted edit. The author must be able to apply a deliberate common invalidation choice without individually operating twenty identical selectors. Also test the assistant below versus beside the document using actual terminal row limits: the browser study can grow vertically, but a terminal cannot. Small-screen browser wrapping is only an interaction study, not the proposed terminal's final pane behavior.

See [ui-verification.md](ui-verification.md) for observed prototype checks and the implementation boundary. A specified command is not necessarily implemented in the interaction study.

## 14. Remaining decisions

- Validate the proposed authored/protected prose policy with real revision work.
- Validate explicit outline expansion before choosing any automatic stopping rule.
- Decide exact history/deletion retention semantics before enabling destructive operations on real projects.
- Choose file schema, stable identifiers, order representation, and terminal framework after the interface experiment.
- Choose default review budgets from measured chapter work rather than the fixture's sample values.
- Confirm which terminal and external editor form the first supported environment.
