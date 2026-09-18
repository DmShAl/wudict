# Working in this checkout

- Read `CLAUDE.md` for the existing project map and conventions; consult the relevant sections of `docs/SPEC.md` before implementation. Do not duplicate those documents here.
- For the current Android UI work, start with `docs/ANDROID-UI-HANDOFF.md`. It maps the touched code, Windows build commands, decisions, and remaining verification.
- The working branch at handoff is `dev`. Check branch/status before editing; preserve user changes and do not switch to the older `flicker_test` branch.
- The user builds from Windows cmd with `build-android.cmd`. Do not commit, publish, or install APKs unless requested. Do not read or print private signing settings/passwords.
- Current user-approved labels (`wuDict`, `Background color`, etc.) take precedence over older naming notes in general documentation.
