# Dictionary groups

Open **☰ → Settings → Edit dictionary groups**. The selector lists
**All Dictionaries**, your groups, and **New Group** last. New groups are empty.
Turn on **Show All** to add dictionaries using the checkboxes; turn it off to
see only members. Members appear first, followed by nonmembers, each in the
existing dictionary order. A dictionary can belong to several groups.

**All Dictionaries** always includes the current collection, including newly
added dictionaries. Its checkboxes cannot be cleared. Removing a dictionary
from a user group does not remove its files; it only changes which group
selects it. This editor does not change search filters; renaming and deleting
groups are outside its scope.

## Storage and API

Groups are additive fields in the existing version 1 `state.json`, beside the
active configuration. `groups` contains `{id, name}` records; each dictionary
preference may contain `groups`, an array of group IDs. Older files need no
rewrite until the next save. Existing order, enabled state and UI preferences
are retained. Existing dictionary ID/path healing also carries membership when
a dictionary moves. Unavailable dictionaries are hidden but their membership
is retained, like their other preferences, so unplugging a drive loses nothing.
All Dictionaries is computed from the registry and is never stored as a list.

Private, authenticated, same-origin endpoints:

- `GET /api/groups`: array of `{id, name, members, readonly}`.
- `POST /api/groups`: `{name}` creates an empty group and returns it.
- `PUT /api/groups/member`: `{group, dict, member}` changes one membership.

Names are trimmed, limited to 100 characters, and checked case-insensitively
against existing names and the reserved All Dictionaries name. Empty names and
control characters are rejected. Saves serialize read/merge/write operations,
write and sync a temporary file, then rename it. A failed save restores memory
and is reported in the editor. Membership never writes the search `off` flag.

## Verification

`go test ./internal/server -run 'TestGroup|TestPrefs|TestOpenAPI|TestRoutes|TestCORSBoundary'`
covers persistence, overlapping membership, automatic collection changes,
validation, failed saves, identity healing and concurrent preference saves.
Run the repository's `make check` on its supported build environment as well.

For manual UI verification, create Essential and Full, enable Show All and add
the same dictionary to both. Turn Show All off, remove a member, reopen the
editor and restart the server. Check that All Dictionaries remains complete,
that an empty group offers the Show All hint, and that Cancel creates nothing.
Check light/dark and custom paper backgrounds on a narrow viewport.
