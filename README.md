# sync-assign

`sync-assign` copies assignments from a teacher-owned Git repository into a
student Git repository. The teacher maps assignment IDs to top-level
directories; a student syncs one assignment at a time.

> [!WARNING]
> Synchronization is one-way: teacher files are copied into the student
> repository. With clean mode enabled, an existing assignment directory is
> removed and replaced. Uncommitted student work blocks replacement unless
> `--force` is also used.

## Requirements and supported platforms

- Git available on `PATH`
- Access to the configured teacher repository
- A student Git repository; commands must run from its root

Release binaries are built for macOS on Apple silicon (`darwin/arm64`) and
Linux on x86-64 (`linux/amd64`).

## Install

Review [`install.sh`](install.sh), then run:

```zsh
curl -fsSL https://raw.githubusercontent.com/ludanortmun/sync-assign/main/install.sh | zsh
```

The installer selects the matching release, verifies its SHA-256 checksum, and
installs `sync-assign` in `~/.local/bin` without `sudo`. Set
`SYNC_ASSIGN_INSTALL_DIR` to install somewhere else.

### Install with Go

If Go 1.26.6 or newer is installed, build and install the latest version with:

```sh
go install github.com/ludanortmun/sync-assign@latest
```

Go installs the binary in `$GOBIN`, or in `$(go env GOPATH)/bin` when `GOBIN`
is unset. Ensure that directory is included in `PATH`.

## Teacher repository

Place each assignment in a single top-level directory and add
`sync-assign.yml` at the repository root:

```text
teacher-repository/
|-- sync-assign.yml
|-- lab-1/
|   |-- README.md
|   `-- starter.go
`-- project/
    `-- README.md
```

```yaml
assignments:
  lab-1: lab-1
  final-project: project
```

Each key is the assignment ID students pass to the CLI. Each value must be the
name of one top-level directory; absolute and nested paths are rejected. The
default teacher branch is `main`.

### Assignment metadata

Each assignment directory also needs its own `sync-assign.yml` file:

```text
teacher-repository/
|-- sync-assign.yml
`-- lab-1/
    |-- sync-assign.yml
    |-- README.md
    |-- requirements.txt
    `-- test_addition.py
```

This per-assignment file is separate from the repository-root
`sync-assign.yml`. It lives inside the assignment directory in the teacher
repository and defines grading metadata for that one assignment.

Minimal example:

```yaml
archetype: python-pytest
due-date: 2026-09-30T23:59:00-07:00
```

Example with overrides:

```yaml
archetype: generic-shell-script
due-date: 2026-09-30T23:59:00-07:00
immutable-files:
  - grade.sh
required-files:
  - grade.sh
  - src/main.txt
checks:
  - file-structure
  - immutable-files-unmodified
archetype-options:
  command: ./grade.sh
```

The exact supported assignment metadata keys are:

| Key | Meaning |
| --- | --- |
| `archetype` | Required grading archetype. Built in: `python-pytest`, `generic-shell-script`. |
| `due-date` | Required due date in RFC 3339 format with an explicit UTC offset, for example `2026-09-30T23:59:00-07:00`. |
| `immutable-files` | Additional relative paths students must not modify. Archetype test files are always protected, even with an explicit list. |
| `timeout` | Positive Go duration per check, e.g. `120s` or `3m`. CLI `--timeout` overrides this; omitted means `120s`. |
| `required-files` | Relative paths that must exist in the student copy. If omitted, every file in the teacher assignment directory is required except the per-assignment `sync-assign.yml`. |
| `checks` | Enabled checks. Supported values: `file-structure`, `immutable-files-unmodified`, `commit-before-due-date`. If omitted or empty, all three are enabled. Immutable-file validation and the unit suite always run; pytest also always checks test-file coverage. |
| `archetype-options` | Optional free-form string map passed to the selected archetype. |

There is no scoring configuration: `gates`, `rubric`, and `points` are not
supported. Unknown YAML keys are rejected. Assignment paths in `immutable-files` and
`required-files` must be clean, slash-separated relative paths.

### Built-in archetypes

#### `python-pytest`

Runs `pytest` for the assignment and reads test results from JUnit XML.

- Requires `requirements.txt` in the assignment directory.
- Creates a fresh, disposable virtual environment per run. This avoids stale
  dependencies and collisions from nested requirements or local packages.
- All discovered cases form one `unit-tests` check. Any failed, errored, skipped,
  or expected-failure (`xfail`) case fails that check; names and reasons are retained.
  No collected tests, setup/collection errors, and nonzero pytest exits also fail it.
- Runs pytest against the assignment directory by default, or against
  `archetype-options.test-path` when set.
- Always protects `test_*.py`, `*_test.py`, `conftest.py`, and pytest configuration
  (`pytest.ini`, `.pytest.ini`, `pyproject.toml`, `setup.cfg`, `tox.ini`) throughout the assignment.
  With `archetype-options.test-path`, every teacher file under that path is also
  protected. Added student test/config files fail the `test-file-coverage` check.
  Pytest file discovery is explicitly restricted to `test_*.py` and `*_test.py`;
  custom `python_files` patterns are not supported.
  Configuration is selected from the assignment root (in pytest's usual order);
  ancestor configuration and ancestor `conftest.py` files are not loaded.

No test IDs need to be configured. Failing-case diagnostics use JUnit identifiers,
for example `test_math::test_addition` or `package.test_math::test_addition`.

#### `generic-shell-script`

Runs a teacher-provided shell command with `/bin/sh -c` and treats specially
formatted stdout lines as test results.

- Requires `archetype-options.command`: a relative script path, such as
  `./grade.sh`, without inline shell arguments. Declare any additional scripts,
  fixtures or helpers it loads in `immutable-files`.
- Collects test results from stdout lines shaped like `PASS <id>` and
  `FAIL <id>`.
- All reported cases form one `unit-tests` check. Any `FAIL` line, nonzero exit,
  or absence of recognized cases fails it. A later `PASS` cannot erase an earlier
  `FAIL` for the same ID. Use nonempty IDs.
- Execution errors are recorded as failed checks with output diagnostics, preserving
  case names and other check results. Pytest collection errors likewise retain output.
- Uses the configured command path itself as the default `immutable-files`
  entry. Because that path is also part of the default required-file set, it
  must exist in the student copy.

### Reproducible grading and time limits

Both commands use a private teacher snapshot from **latest `main`**, ignoring
the student's sync branch and mirror-path settings. To grade against an older
teacher configuration and immutable baseline, use a commit ID:

```sh
sync-assign check lab-1 --teacher-commit abcdef123456 --timeout 3m
sync-assign grade lab-1 --teacher-commit abcdef123456 --timeout 3m
```

`--teacher-commit` accepts an unambiguous 7–40 digit hexadecimal commit ID
available in the cloned teacher history (not branch names or revision expressions).
Configuration, assignment mappings, checks and baseline all come from that same
commit. No shared teacher checkout is reset or updated. Snapshot files are read
directly from Git blobs without EOL conversion or checkout filters. Teacher
repositories containing symlinks or submodules are currently unsupported.

Timeout precedence is `--timeout` > assignment `sync-assign.yml` `timeout` >
`120s`. Durations must be positive. Each check receives its own deadline; the
unit suite receives one deadline including dependency installation.
Timeout kills the subprocess group and records failure diagnostics.
Git/network preparation is outside the check timeout.
Failed checks never prevent remaining checks from executing, including when immutable
files differ. Setup and execution failures are retained alongside other results.
Grade reports are first-pass feedback for teacher review,
not a sandbox or an automatic LMS verdict. Run student code only in an appropriate
isolated environment; custom imported helpers must be declared immutable.

### Checks and pass/fail

Every configured or mandatory check must pass for the assignment to pass.
There are no individual scores, points, partial credit, or rubric entries.
Checks execute in sequence, without short-circuiting on failure.

The built-in checks are:

| Check | Meaning |
| --- | --- |
| `file-structure` | Every required path and immutable path is present in the student assignment directory. |
| `immutable-files-unmodified` | Every immutable file matches the teacher Git blob byte for byte. Whitespace, indentation, line endings and final newlines must not change. Symlink paths are rejected. |
| `commit-before-due-date` | The graded commit time is on or before the assignment due date. |
| `test-file-coverage` | For pytest, student test/configuration paths must be covered by the immutable baseline. Always enabled for pytest. |
| `unit-tests` | The entire discovered test suite passes, with at least one case and no failures or skips. Always enabled. |

A specific test failure fails `unit-tests` and the assignment, and the report names
the failing cases. Other check results remain visible even after errors or timeouts.

## Student setup

From the root of a student Git repository:

```sh
sync-assign init-student https://github.com/example/course-assignments.git
```

If the terminal is interactive, omitting the repository argument prompts for
it. The command creates `.sync-assign.yml` and refuses to overwrite an existing
file unless `--force` is supplied.

```yaml
teacher-repository: https://github.com/example/course-assignments.git
commit: true
clean: false
ephemeral: false
branch: main
```

The exact supported student configuration keys are:

| Key | Meaning |
| --- | --- |
| `teacher-repository` | Required teacher Git URL or local repository path. |
| `commit` | Create a local commit after a successful sync. Default: `true`. |
| `clean` | Permit replacement of an existing assignment. Default: `false`. |
| `teacher-path` | Use this local path for the teacher mirror. |
| `ephemeral` | Clone into a temporary directory and remove it afterward. Default: `false`. |
| `skip-mirror` | Compatibility alias for `ephemeral`; if both are set, their values must agree. |
| `branch` | Teacher branch to clone or update. Default: `main`. |

`teacher-path` and an enabled `ephemeral` mode cannot be used together.
Unknown YAML keys are rejected.

## Sync an assignment

```sh
sync-assign lab-1
```

The assignment ID is looked up in the teacher's `sync-assign.yml`. Command-line
options override `.sync-assign.yml` for that invocation.

### Sync flags

| Flag | Behavior |
| --- | --- |
| `--[no-]commit` | Enable or disable the local commit after syncing. |
| `--[no-]clean` | Enable or disable replacement of an existing assignment. |
| `--force` | Allow clean mode to replace an assignment that has uncommitted changes. Requires clean mode. |
| `--mirror-path=PATH` | Override the local teacher mirror path and disable ephemeral mode. |
| `--[no-]ephemeral` | Enable or disable a temporary teacher clone; enabling it clears a configured mirror path. |
| `--branch=BRANCH` | Override the teacher repository branch. |
| `-m, --message=TEXT` | Set the commit message; the default is `Sync assignment <id>`. |
| `--version` | Print version information and exit. |
| `-h, --help` | Show help. |

Without clean mode, syncing fails if the target assignment directory already
exists. Clean mode replaces that whole directory, but first refuses to proceed
if Git reports tracked or untracked changes within it. `--force` bypasses only
that dirty-worktree protection and is invalid unless clean mode is enabled.

By default, a successful sync stages changes under the assignment directory
and creates a local commit containing only that directory. Unrelated staged
changes remain in the index, and the sync does not push the commit. Set
`commit: false` or pass `--no-commit` to opt out.

### Check an assignment

```sh
sync-assign check lab-1
```

`check` runs the grading pipeline against the current working tree of the
already-synced assignment directory in the student repository. Run
`sync-assign <id>` first so the assignment directory exists locally. The
subcommand takes only the assignment ID.

The report is a preview, not an authoritative grade. It evaluates the same
default or configured checks as grading, except `commit-before-due-date` is
omitted entirely from the report, since `check` does not grade a historical
commit and there is nothing meaningful to evaluate that check against.

Example report:

```text
PREVIEW - not authoritative
Assignment: lab-1
Status: PASS
Checks:
- [PASS] file-structure: all required paths are present
- [PASS] immutable-files-unmodified: immutable files match the teacher copy
- [PASS] test-file-coverage
- [PASS] unit-tests
```

For a failing suite, the last line might instead be:

```text
- [FAIL] unit-tests: test_addition::test_sum: assert 3 == 4
```

The overall status would be `FAIL`. `check` exits nonzero if any check fails.

> [!NOTE]
> `check` is for student-side feedback. Its preview report is intentionally
> non-authoritative because it runs against the current working tree instead of
> a commit selected by due date.

### Grade an assignment

```sh
sync-assign grade lab-1
```

`grade` is the authoritative teacher-facing grading command. The subcommand
takes only the assignment ID.

For the current local branch in the student repository, it:

1. Fast-forwards the local branch to match its remote counterpart.
2. Finds the newest commit at or before the assignment due date.
3. Checks out that commit into a temporary Git worktree.
4. Runs the full grading pipeline there with all enabled checks, including
   `commit-before-due-date`.
5. Prints the graded commit and the report, then removes the temporary
   worktree.

Unlike `check`, `grade` exits nonzero only when grading could not be completed
at all (for example, invalid configuration or failed Git preparation).
A completed report with failed checks, including test setup/execution errors, produces a
successful grading run and an exit code of zero.

> [!WARNING]
> `grade` fast-forwards the student's current local branch before grading. If
> the local branch has diverged from its remote and cannot be fast-forwarded,
> grading stops with an error.
> Deadline selection uses the fetched remote commit's history, not local
> `HEAD`. Local-only commits are preserved but never graded.

### `init-student` flags

```text
sync-assign init-student [<teacher-repo>] [flags]
```

`--[no-]commit`, `--[no-]clean`, `--mirror-path`, `--[no-]ephemeral`, and
`--branch` write the corresponding defaults to `.sync-assign.yml`.
`--force` overwrites an existing student configuration.

## Teacher mirror behavior

By default, the teacher repository is kept as a persistent mirror and updated
from its configured branch on every sync. Its path is deterministic:

```text
$XDG_CACHE_HOME/sync-assign/<sha256-of-teacher-repository>
```

If `XDG_CACHE_HOME` is unset, the operating system's user cache directory is
used. `XDG_CACHE_HOME`, when set, must be an absolute path.

Set `teacher-path` or pass `--mirror-path` to choose a persistent location.
The path is cloned when absent; an existing path must be a Git worktree.

Set `ephemeral: true` or pass `--ephemeral` to clone the teacher repository
into a temporary directory for that invocation. The temporary mirror is
removed when synchronization finishes.
