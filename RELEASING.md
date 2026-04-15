# Releasing Station Manager (v2)

**v2 has no release process yet.** It is under construction on the `main`
branch as of 2026-04-15 and has not reached a releasable milestone.

The v1 release process (Taskfile + Wails build + nfpm packaging via GitHub
Actions) is preserved on the `v1` branch along with the `v1.0.0` tag. To see
how v1 was released:

```
git checkout v1
cat RELEASING.md
```

Tag-triggered workflows on GitHub run against the workflow files present at
the tagged commit, so pushing a `v1.x.y` tag from the `v1` branch will use
v1's preserved `.github/workflows/release.yml` even though `main` no longer
has it.

The v2 release process will be designed and documented when v2 reaches a
state where it can be shipped. This will likely involve a new GitHub Actions
workflow, a revised Taskfile, and decisions about packaging format
(`.deb` / `.rpm` / Flatpak / something else). None of these are settled yet.
When a v2 release process is designed, it will appear as
`docs/v2-design/release-process.md`.
