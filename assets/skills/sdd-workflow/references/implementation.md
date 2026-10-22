# Apply One Task and Stop

Use only when the user authorizes implementation of a specific pending task.
Approval of specs, plans, or tasks does not itself authorize execution.

1. Establish the project root and exact spec, plan, and tasks from the same
   feature. Identify one exact task ID, RFs, dependencies, allowed files/areas,
   policy, agreed verification, and its completion criterion. Require completed
   dependencies and explicit authority to implement and mark this task only.
   Check that relevant plan/tasks approvals still apply to the current spec and
   that no requirement-change reconciliation is pending for this task. Do not
   infer freshness from file existence; unresolved linkage blocks affected work.
   You own these checks: the task exists exactly once in tasks.md, is unchecked,
   its dependencies are complete, its RFs exist in the spec, and it has a clear
   `@@DONE_WHEN@@` criterion. A failed check blocks delegation.
2. Delegate to `sdd-apply` by reference, never by paraphrase. Its model is
   cheaper, so the assignment must be closed:

   ```text
   Feature: <root>/ai/specs/<feature>/ · Task: <ID> · RF: <IDs>
   Read: spec.md (full); tasks.md (row <ID> + the policy/verification section);
   plan.md sections "<exact headings>"; docs/constitution.md sections
   "<exact headings>" (minimal change, honest verification, security).
   testing_mode: <none|tests|tdd> · Approval: implement <ID> and mark its checkbox
   ## Allowed edit surfaces
   ## Skills to load before work      (exact SKILL.md paths, e.g. code style)
   ## Relevant context                (forwarded memory, decisions, path:line)
   ## Verification                    (exact commands from tasks.md, each as
                                      `./gradlew -q <task> 2>&1 | tail -60`
                                      {{#opencode}}with workdir at the Gradle root){{/opencode}}{{#other}}from the Gradle root){{/other}}
   ```

   Launch it in a NEW child session; never reuse a child from another task.

   Name sections by heading, not line numbers. Forward relevant memory; the
   child has none. Require preservation of local changes, no other task, no new
   decisions/dependencies, and no commits, publishing, destructive operations,
   or recursive delegation.
3. The child checks the baseline, applies only the task, and verifies according
   to policy. With testing, required initial failures block unrelated fixes and
   final red/omitted checks prevent marking. With `none`, it neither creates nor
   runs tests, checks agreed criteria, and discloses their omission honestly.
4. Review the child's report, actual diff, task checkbox, and evidence. Confirm
   that only the assigned task's checkbox changed, source docs were preserved,
   unrelated work survived, and completion criteria are met. Check the RFs the
   child reports as covered and not covered. Missing required evidence is
   blocked, not a successful handoff. Do not trust a checkbox alone. If the child
   returns `interaction_required`, present its options unchanged and stop.
5. Report completed or blocked with task/RFs, files, commands/results if executed,
   policy, and limitations. A completed task without testing is not proof of test
   validation. Stop; do not launch another task until the user authorizes it.
