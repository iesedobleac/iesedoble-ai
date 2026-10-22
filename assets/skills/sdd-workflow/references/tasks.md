# Task Decomposition

Use this workflow when the user requests a tasks document for an approved plan.
Do not automatically create it after saving a plan or treat task creation as
authorization to implement. Preserve all existing spec and review workflows.

1. Establish the root, exact spec and plan versions, and confirmation that the
   plan is approved. They must belong to the same feature. Respect AGENTS.md
   already in context without redundant reads; read the constitution, spec, and
   plan explicitly. Missing approval or unresolved decisions are blockers.
2. The sole destination is `<project-root>/ai/specs/<spec-name>/tasks.md`, beside the
   spec and plan. Validate the destination and inspect existing tasks; preserve
   local work, IDs, and progress. Do not overwrite or reset checkboxes silently.
3. Delegate to `sdd-tasks` with sources, root, plan approval, confirmed decisions,
   constraints, scope, existing tasks if any, and destination. Initially authorize
   analysis and drafting only: the child writes the full draft to
   `@@OPENCODE_TMP@@/sdd-drafts/<feature>-tasks-<n>.md` and returns a summary
   (about 2k tokens or less) plus the path. Explicitly prohibit execution and
   code changes. Batch row changes into one assignment per approved revision.
4. Relay any consequential question individually. Return functional gaps to the
   relevant spec procedure and technical gaps to `planning.md` only when that
   review is needed; do not invent decisions during decomposition or reset earlier
   question limits.
5. Review task IDs, unchecked new checkboxes, realistic estimates strictly under
   30 minutes including required verification, exact RF associations, and one
   verifiable completion line per task (`@@DONE_WHEN@@`). Check that
   dependencies exist and appear earlier, with no cycles or self-dependencies.
   Review RF coverage and all verification promised by the plan. Reject invented
   scope, decisions, dependencies, or task-completion claims. An indivisible
   operation that exceeds the limit is a blocker, not permission to understate it.
6. Check policy consistency: `none` has no test tasks or test-required criteria;
   include only agreed non-test checks. `tests`/`tdd` preserve complete-suite green
   and agreed platform gates. Only `tdd` is test-first; never complete a RED-only
   task or advance while required tests fail. Missing required evidence blocks.
7. Present the draft path, its summary, and the coverage table. Obtain explicit approval
   of BOTH the exact content and saving it, including any existing-file revision.
   Relay the approval and draft path to `sdd-tasks` for writing that
   file only. Require source/destination rechecks; changed content stops writing
   for review. Do not use `android-worker` as the default writer in this workflow.
8. Read the saved file and verify approved content, destination, estimates, RF
   coverage, dependency order, and checkboxes. Report actual checks and limits.
   Tasks remain unexecuted; saving the checklist does not authorize implementation,
   marking tasks complete, commits, or publication.
