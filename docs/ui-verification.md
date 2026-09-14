# UI interaction study: verification and limits

This records the earlier browser study. See [test-verification.md](test-verification.md) for the runnable terminal build.

September 14, 2026. These checks exercised deterministic sample data in the in-app browser. They validate specific interface transitions, not real model quality, filesystem safety, terminal compatibility, or author usability.

## Exercised successfully

- Browse from a scene to a knowledge entry without retargeting the existing assistant conversation.
- Start a conversation explicitly for the knowledge entry and apply a sample edit to that entry.
- Enter inserts a prompt newline without sending. Send applies the sample turn.
- Select a text-only manual model; change to continuity research; resolve to a tool-capable model. The consistency-role selector disables the text-only fixture.
- Show both story and chapter style instructions; change the local prose-model assignment.
- Turn off automatic knowledge selection while leaving outline selection on and required attachments present.
- Refuse Finish editing while saved changes lack invalidation choices; finish after explicit choices.
- Navigate with a dirty text buffer, return through Keep editing, preserve its content, and save with Ctrl+S while the pipeline stays paused.
- Add a child to a leaf with prose, choose private-note retention, and verify the old parent prose no longer appears in the manuscript.
- Import generated detail inside a brief without creating child jobs or duplicate navigator items.
- Import a proposed child through the same existing-prose decision used by manual child creation.
- Cancel active sample generation before editing; canceled callbacks do not later resume the pipeline.
- Reach the sample generation limit; retain a candidate and show review findings with a knowledge-source link.
- Open the command chooser, filter to Help, activate it, and dismiss with Escape. The current terminal implementation binds the chooser to Ctrl+G.
- Switch to the navigator in compact layout, choose an item, and return to its document.
- Start a second conversation, then restore the first conversation and its original scope.
- Inspect light and dark layouts at browser widths of 1024, 736, and 360 pixels. The normal workspace and narrow context controls fit their containers. The narrow help dialog fits without horizontal overflow.
- Parse the fragment's JavaScript with Node and check the browser log for runtime errors during the exercised flows.

During verification, fixes included preserving dirty buffers on navigation, making knowledge conversations target knowledge entries, applying the leaf-conversion rule to child imports, retaining separate sessions, showing inherited style/attachments, and preventing status labels from squeezing navigator titles in compact layouts.

## Deliberate prototype limits

- Model responses, outline proposals, reviews, tool capability, and progress are fixtures. No provider calls or project writes occur.
- Source/candidate history is illustrative. It is not durable version storage, a dependency graph, a correctness check, or a file transaction system.
- New/Open project, external editor, export, and provider setup display their intended surfaces without connecting external operations.
- Move, reorder, deletion recovery, bulk invalidation decisions, combined branch-plus-selection scope, exact style replacements, and configurable research permissions are specified for the real interface but not fully implemented in this study.
- The sample invalidation scope and budgets demonstrate choices; they do not execute a complete outline/context dependency cascade or enforce real token/cost accounting.
- A browser textarea demonstrates field focus and editing. Terminal mouse reporting, cursor placement for wide characters, paste handling, key conflicts, pane scrolling, compact pane switching, and editor return must be checked in a terminal implementation.
- The browser study does not prove a comfortable workflow in a 24-row terminal. Its height can grow, and only recent sample conversation messages are displayed.
- No author usability trial or writing-quality comparison has been performed. Twenty-prompt editing sessions and real chapter revisions are the next meaningful evaluation.
