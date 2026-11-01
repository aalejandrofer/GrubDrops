# Release notes format

Never a paragraph wall. Lead with a one-line **highlight**, then group entries
under these emoji section headers, in this order (omit empty sections):

- **🌱 Added** — new features
- **⚙️ Changed** — behaviour / UX changes
- **🐛 Fixed** — bug fixes
- **🔥 Removed** — removed features, flags, or paths

Bold the subject of each bullet; write what changed + why it matters in plain
user-facing language, not commit-speak. The notes are a curated subset of
`[Unreleased]`, not the raw changelog.

Release **title** = bare version only (e.g. `v1.2.5`). Release **notes** are
humanized (run the humanizer skill): plain voice, **no em/en dashes**, but keep
the emoji-section + bold-subject format above.
