# Working in this checkout

- Read `CLAUDE.md` for the existing project map and conventions; consult the relevant sections of `docs/SPEC.md` before implementation. Do not duplicate those documents here.
- For the current Android UI work, start with `docs/ANDROID-UI-HANDOFF.md`. It maps the touched code, Windows build commands, decisions, and remaining verification.
- The current dictionary-group work is on `dictionary-groups` (handoff snapshot in `docs/ANDROID-UI-HANDOFF.md`). Check branch/status before editing; preserve uncommitted work and user changes. Work directly in this checkout unless the user asks otherwise; do not apply old stashes or patches without inspecting them.
- The user builds from Windows cmd with `build-android.cmd`. Do not commit, publish, or install APKs unless requested. Do not read or print private signing settings/passwords.
- Current user-approved labels (`wuDict`, `Background color`, etc.) take precedence over older naming notes in general documentation.
