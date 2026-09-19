# Working in this checkout

- Read `CLAUDE.md` for the existing project map and conventions; consult the relevant sections of `docs/SPEC.md` before implementation. Do not duplicate those documents here.
- For the current Android UI work, start with `docs/ANDROID-UI-HANDOFF.md`. It maps the touched code, Windows build commands, decisions, and remaining verification.
- Check branch/status before editing; preserve uncommitted work and user changes. Branch names in handoff snapshots are historical context, not instructions to switch branches. Work directly in this checkout unless the user asks otherwise; do not apply old stashes or patches without inspecting them.
- The user builds from Windows cmd with `build-android.cmd`. Do not commit, publish, or install APKs unless requested. Do not read or print private signing settings/passwords.
- Current user-approved labels (`wuDict`, `Background color`, etc.) take precedence over older naming notes in general documentation.

## Git branch policy

- `dev` is the development integration branch. Create new feature, fix, and experimental branches from `dev`, not from `master` or an unrelated task branch, unless the user explicitly requests another base.
- `master` is reserved exclusively for synchronization with the upstream project from which this repository was forked. Do not develop fork-specific features or fixes on `master` or merge task branches into it.
- Before creating a branch, verify the base and working-tree status. Do not silently switch, reset, rebase, stash, or discard existing work to enforce this policy. Existing task branches are not retroactively rebased. If `dev` is missing or its intended state is unclear, clarify rather than falling back to `master`.
- When a task is ready, its integration target is `dev`; committing, merging, publishing, and upstream synchronization still require the user request specified above.
