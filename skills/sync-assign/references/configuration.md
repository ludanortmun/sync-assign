# Configuration and teacher authoring

## Teacher repository

The root file is `sync-assign.yml` (no leading dot). Each assignment occupies a
single top-level directory. Mapping keys are CLI assignment IDs:

```yaml
assignments:
  handout: reading
  lab-1:
    path: python-lab
    archetype: python
  final-project:
    path: project
    archetype: jupyter
```

Here `sync-assign lab-1` copies `python-lab/`, not `lab-1/`. The scalar `handout`
entry can be synced but cannot be checked or graded. A mapping needs `path`;
set `archetype` to `python` or `jupyter` to enable checking and grading.
Unknown YAML keys and unsupported archetypes are rejected.

Paths must be single directory names: no absolute paths, nested paths, `.` or
`..`, backslashes, or `.git`. Create the actual directory in the teacher repo.
Publish the assignment and config on the intended teacher branch through the
user's normal Git workflow; do not infer authorization to push from authoring.
The default teacher branch is `main`.

For gradable assignments:

- Supply Python tests in the assignment. `requirements.txt`, when present, is
  loaded automatically; `pytest` and `nbmake` are supplied by the checker.
- Jupyter's main notebook tests target `notebooks/`. Students can insert cells
  but must preserve teacher cells' types and sources in their original order.
  Deliver notebooks with cleared outputs and null execution counts.
- Teacher files anywhere under a `tests` directory and files named `test_*.py`
  are integrity-protected. Put intended student edits outside those files.
- Extra-credit Python filenames end in `extra_credit.py`; extra-credit notebooks
  match `notebooks/*_extra_credit.ipynb`. See [grading](grading.md) for exact
  pipeline order and non-blocking behavior.

## Student repository

The default root file is `.sync-assign.yml` (leading dot). These are all supported
keys; omitted optional keys use the defaults below:

```yaml
teacher-repository: https://github.com/example/course-assignments.git
commit: true
clean: false
ephemeral: false
branch: main
```

| Key | Meaning and default |
| --- | --- |
| `teacher-repository` | Required Git URL or local repository path. Prefer absolute paths for local repos, especially with shared config. |
| `commit` | Create a local assignment commit after sync; default `true`. |
| `clean` | Replace an existing assignment directory; default `false`. |
| `branch` | Teacher branch; default `main`, including during check/grade. |
| `teacher-path` | Optional persistent mirror path. Relative paths resolve from the command's working directory, not the config's directory. |
| `ephemeral` | Use a temporary teacher clone and remove it afterward; default `false`. |
| `skip-mirror` | Compatibility alias for `ephemeral`; when both are set their values must agree. Prefer `ephemeral` in new configs. |

Unknown keys are errors. `teacher-path` cannot coexist with enabled ephemeral
mode. Flags override configured values for one invocation; absent flags retain
configuration values, then built-in defaults.

All commands accept `--config=PATH`. An absolute config path is used as supplied;
a relative path resolves from the student Git root, even when the config lives
elsewhere. Create the parent directory before writing a custom config path.
For sibling student repos, a shared config can live at `../.sync-assign.yml`.

`init-student` accepts `--[no-]commit`, `--[no-]clean`, `--mirror-path=PATH`,
`--[no-]ephemeral`, and `--branch=BRANCH` to write defaults. Its `--force`
overwrites the configuration; it does not force an assignment replacement.

## Teacher mirrors

A mirror is a disposable Git worktree used as a source, not an authoring copy.
Sync, check, and grade prepare it from the current teacher branch. Existing
mirrors are fetched, force checked out, hard reset, and cleaned with
`git clean -ffdx`, discarding tracked, untracked, and ignored local changes.

The default persistent path is
`CACHE_ROOT/sync-assign/SHA256(teacher-repository)`, using the hexadecimal SHA-256
of the configured repository string. `CACHE_ROOT` is `$XDG_CACHE_HOME` when set
(it must be absolute), otherwise the operating system's user cache directory.

- `teacher-path` or `--mirror-path=PATH` selects a persistent location. The
  location is cloned when absent; an existing location must be a Git worktree.
  Use a dedicated disposable directory, never a teacher or student authoring repo.
- `--mirror-path=PATH` disables configured ephemeral mode.
- `--ephemeral` uses a temporary clone, removes it afterward, and clears a
  configured mirror path. `--no-ephemeral` selects persistent behavior.
- Passing both `--mirror-path` and enabled `--ephemeral` is an error.
- Persistent mirrors survive the command; temporary mirrors do not. Retaining a
  grading worktree does not retain an ephemeral teacher mirror.
