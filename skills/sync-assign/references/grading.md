# Grading and checker behavior

## Select the submission

Run from one student's Git repository root. `check` evaluates current files;
`grade` creates a detached temporary worktree for the latest commit on the
selected local student branch at or before the cutoff:

```sh
sync-assign grade lab-1 --branch=main --due=2026-09-18T23:59:59-07:00
```

The branch defaults to the checked-out student branch. Detached HEAD requires
an explicit `--branch`. Uncommitted and untracked files are absent from grading;
ignored assets are absent unless committed or explicitly mounted. A commit must
exist on that branch at or before the cutoff.

`--due` accepts RFC3339 with an explicit offset or `YYYY-MM-DD`. Date-only input
means the end of that day in the **machine's local time zone**. Omission uses
the current time. Prefer an explicit offset when the requested deadline's zone
is known; do not silently substitute the machine's zone for the course's zone.

The teacher integrity baseline is the current configured teacher branch, even
when the student commit is old. Use `--teacher-branch=BRANCH` to select another
teacher branch; `--branch` selects the student branch during grading.

`--pull` is optional and changes local student branch state using `origin` before
selecting the submission. Use it when updating submissions is part of the task.
The current branch is fetched and fast-forwarded only; another local branch is
updated by fetch, which rejects non-fast-forward changes. A rejection is not
permission to reset or force-update the branch.

For sibling student repos with shared teacher configuration, enter each repo
separately and pass the shared path:

```sh
cd students/alan
sync-assign grade lab-1 --config=../.sync-assign.yml --due=2026-09-18T23:59:59-07:00
```

## Mount local assets

Use repeatable `--mount=SOURCE:DESTINATION` to supply datasets or models absent
from the historical commit:

```sh
sync-assign grade lab-1 --due=2026-09-18T23:59:59-07:00 \
  --mount=./python-lab/data:data \
  --mount=/srv/datasets/models:assets/models
```

`SOURCE` must be an existing directory. Relative sources resolve from the student
Git root; absolute paths and sources outside the repo are allowed. Source
symlinks are resolved. `DESTINATION` is relative to the mapped assignment
directory inside the grading worktree, not to the worktree root.

- Destinations must not exist, including dangling symlinks. Missing parents are
  created, but existing parents cannot be symlinks or files.
- Destinations cannot be absolute, target the assignment root, contain `..` or
  `.git` components, or contain colons. Duplicate and overlapping targets fail.
- The final colon separates source and destination. Quote the full flag value
  when paths contain spaces, e.g. `--mount="/srv/course data:assets/data"`.
  There is no `:ro` mount mode.

Mounts are **live, writable directory symlinks**, not historical snapshots or
copies. Writes by tests affect the source. Choose disposable copies when the
task requires isolated writes. The report lists resolved sources and relative
destinations under `mounts:`; without mounts it reports `mounts: none`.
Cleanup removes links and preserves sources.

## Inspect a grading worktree

`--keep-worktree` prints `retained worktree: PATH` immediately after creation and
keeps it even when checks or mount setup fail. Inspect that printed path; do not
guess a temporary location. Mounted assets remain live links. Without this flag,
the temporary worktree is removed after the run. Creation requires write access
beside the student repository, where the `.sync-assign-grade-*` container lives.

When inspection is complete and removal is authorized, run from the student root:

```sh
git worktree remove --force "PATH_PRINTED_BY_GRADE"
```

Replace the placeholder with the actual retained worktree path. This removes
generated or modified worktree files too; preserve any wanted artifacts first.
Then use `rmdir` on its now-empty `.sync-assign-grade-*` parent directory. Do not
remove mount source directories. Teacher mirror cleanup is independent.

## Checker pipeline and results

Checks continue after failures and run in this order:

| Archetype | Ordered checks |
| --- | --- |
| `python` | Test-file integrity; main Python tests; extra-credit Python tests |
| `jupyter` | Test-file integrity; cleared notebook outputs/counts; unchanged teacher cells; main Python tests; extra-credit Python tests; main notebook tests; extra-credit notebook tests |

Integrity requires every teacher-supplied file under any `tests` directory and
every teacher `test_*.py` to retain its type and contents. For notebooks,
teacher cells must remain an ordered subsequence with the same cell types and
sources; students may insert cells. Clear saved outputs and set execution counts
to null. `.ipynb_checkpoints` directories are excluded from notebook integrity
checks.

Python tests use `uv run` with `pytest`. Notebook tests use `pytest --nbmake` with
`nbmake`. Both load assignment `requirements.txt` when present. These tools can
download dependencies and run assignment code. In `check`, that code runs in
the student's working directory.

Main Python tests exclude filenames ending in `extra_credit.py`; those are run
separately. Main notebook tests target `notebooks/` and exclude
`notebooks/*_extra_credit.ipynb`; matching extra-credit notebooks run separately.
Jupyter main Python tests are skipped without a `tests/` directory. Python
extra-credit tests run when matching files exist, including without `tests/`.
Absent extra-credit tests/notebooks are skipped.

Extra-credit failures are non-blocking: the summary may show failed checks with
`result: passed` and exit status zero. Required check failures produce
`result: failed` and a nonzero status. Skips are reported separately. Reports
contain ANSI colors; failed checks and their details go to stderr, other states
and the summary to stdout. Pytest failure details contain its short test summary,
without tracebacks. Report the selected student branch/commit, cutoff, mounts,
required failures, extra-credit failures, and setup errors when relevant.
