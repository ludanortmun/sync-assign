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
