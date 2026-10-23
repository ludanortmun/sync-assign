---
name: sync-assign
description: Use the sync-assign CLI to configure teacher and student assignment repositories, sync assignments, check current student work, and grade committed submissions. Use for sync-assign workflows, not generic file synchronization or developing the CLI itself.
---

# sync-assign

`sync-assign` copies one assignment from a teacher Git repository into a student
Git repository. Checking evaluates current student files; grading evaluates a
committed submission against the current teacher template.

## Establish the inputs

- Require `sync-assign` and Git on `PATH`; checking and grading also need `uv`.
  If the CLI is missing, use the project's [installation instructions](https://github.com/ludanortmun/sync-assign#install).
  With Go 1.26.6 or newer, the Go installation command is
  `go install github.com/ludanortmun/sync-assign@latest`; its binary directory
  must be on `PATH`. Installing the skill does not install the CLI.
- Run all commands from the **student Git repository root**, including grading
  and setup. Compare the current directory with `git rev-parse --show-toplevel`.
  An assignment subdirectory or the parent of multiple student repos is invalid.
- Read `.sync-assign.yml`, or the user-selected `--config=PATH`, before choosing
  flags. Relative config paths resolve from the student repository root.
  Preserve configured defaults unless the task calls for an override.
- Identify the assignment ID from the teacher's `sync-assign.yml` on the
  configured teacher branch. Read an accessible teacher checkout or a dedicated
  clone of that branch when needed. IDs are mapping keys, and may differ from
  directory names. There is no assignment-list command; do not probe guessed IDs.
- Read [configuration and teacher authoring](references/configuration.md) when
  creating or changing either configuration, selecting a mirror, or preparing
  assignments. Never use a teacher authoring checkout as a mirror: existing
  mirrors are fetched, reset, and cleaned, including untracked and ignored files.

## Choose the operation

Examples use `lab-1` as a placeholder for a confirmed teacher assignment ID.

| Goal | Command | Evaluated or changed state |
| --- | --- | --- |
| Configure a student repo | `sync-assign init-student TEACHER_REPO` | Writes student configuration |
| Copy the teacher assignment | `sync-assign lab-1` | Copies files; creates a local commit by default |
| Validate work before committing | `sync-assign check lab-1` | Current files, including modified and untracked files |
| Evaluate a committed submission | `sync-assign grade lab-1` | Latest student commit at or before now |

For noninteractive setup, always pass the teacher Git URL or local path:

```sh
sync-assign init-student https://github.com/example/course-assignments.git
sync-assign lab-1
sync-assign check lab-1
```

Setup requires an existing student Git repository. It refuses to overwrite an
existing config unless `--force` is passed. Reuse existing configuration rather
than reinitializing it. For local teacher repositories, prefer an absolute path.

## Sync without losing student work

Sync is one-way and copies the directory mapped to the ID. It does not merge
teacher changes into student solutions. By default `commit` is true and `clean`
is false; configured values and explicit flags can change those defaults.

- Without clean mode, an existing assignment target causes an error.
- Clean mode replaces the **whole directory**, including committed solutions.
  Use it only when replacement is within the user's authorized task. Inspect
  `git status --short --ignored -- ASSIGNMENT_PATH` first. Tracked changes,
  untracked files, and ignored files all block replacement without force.
- Never automatically retry with `--force`. It requires clean mode and bypasses
  dirty-worktree protection. Only use it when discarding those files is already
  authorized. Do not delete, stash, or reset work just to make sync succeed.
- A successful committing sync stages and commits only the assignment directory,
  preserves unrelated staged changes, and never pushes. Use `--no-commit` when
  the requested workflow needs to inspect or edit copied files before committing.
  The default message is `Sync assignment <id>`; `-m TEXT` or `--message=TEXT`
  sets a custom message for that sync.

```sh
sync-assign lab-1 --no-commit
```

This is an alternative initial-sync invocation, not a second step after a sync.
For an authorized replacement, use `sync-assign lab-1 --clean`; do not add force
unless discarding dirty files is also authorized.

## Check and grade

Both commands require a teacher assignment archetype of `python` or `jupyter`.
A scalar assignment mapping is sync-only. Checking runs tests in the current
assignment directory without staging or committing; test tools may create caches
or generated files. Do not substitute grading for checking uncommitted work.

Read [grading and checker behavior](references/grading.md) for historical
submissions, cutoffs, shared configuration, mounts, retained worktrees, notebook
rules, and the checker pipeline.

| Operation | Teacher branch override | Student branch override |
| --- | --- | --- |
| Sync or setup | `--branch=BRANCH` | None; uses the current student checkout |
| Check | `--teacher-branch=BRANCH` | None; checks current files |
| Grade | `--teacher-branch=BRANCH` | `--branch=BRANCH` |

Capture both stdout and stderr and the process exit status. Reports contain ANSI
colors, progress states, a summary, and a final `result`. Failed checks go to
stderr. A failed extra-credit check increments the failure count but does not
fail the assignment; report it separately. Skipped checks are not passes. Setup,
Git, configuration, or cleanup errors can fail the command independently of the
checks, so retain the diagnostic even if a report says `passed`.

## Recover from a known cause

| Symptom | Next step |
| --- | --- |
| Missing `sync-assign`, Git, or `uv` | Resolve the missing prerequisite and `PATH`; do not replace the workflow with guessed commands. |
| Missing student config | Look for a supplied shared config; otherwise initialize with the known teacher repository. Ask for the repository only if it cannot be found. |
| Unknown assignment ID | Read the teacher mapping on the selected teacher branch; check the ID and branch before retrying. |
| Archetype is not set | The teacher mapping is sync-only. A teacher may add a supported archetype; do not change teacher configuration as a student workaround. |
| Existing target or dirty-worktree refusal | Inspect the target and user intent. Keep the existing work unless its replacement is authorized. |
| Sync staging or commit failure | Copying may already have succeeded. Inspect files, `git status`, and recent commits before retrying. Resolve the reported identity, hook, or Git issue and finish only the intended assignment commit; do not re-sync with clean/force. |
| Check failure | Use checker details to distinguish test, notebook, integrity, and environment failures. Preserve teacher tests and notebook cells; do not weaken them to obtain a pass. |

Use `sync-assign --version` and the relevant command's `--help` to diagnose an
installed version that differs from this guidance. Do not invent `--json`,
`--dry-run`, or flags shared by unrelated commands.
