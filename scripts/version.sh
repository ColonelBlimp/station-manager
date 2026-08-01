#!/usr/bin/env bash
# Shared build-version helpers. SOURCE this file (`. scripts/version.sh`);
# it defines functions and runs nothing on its own. POSIX-sh compatible so
# it can be sourced from the bash build scripts and from Taskfile `sh:` vars.
#
# The version flows two places that differ in what they accept:
#   - `internal/buildinfo.Version` (via -X) → daemon User-Agent, ADIF
#     PROGRAMVERSION, GET /v1/version, and the `version` field on EVERY log
#     record. Wants the readable semver-ish string. NB: -X on a nonexistent
#     symbol exits 0 and stamps NOTHING, so the old `-X main.Version=` silently
#     produces a `dev` build.
#   - the RPM `Version:` field → cannot contain '-' (the NVR separator), so
#     it needs sanitising.

# sm_git_version prints the readable build version derived from git: the
# nearest v2.* tag with the leading 'v' stripped, e.g. 2.0.0-alpha.1, or
# 2.0.0-alpha.1-3-g8ad4ed5e (3 commits past the tag), with a -dirty suffix
# when the tree has uncommitted changes. Falls back to a short SHA, then to
# 0.0.0-unknown outside a git checkout.
sm_git_version() {
    local desc
    desc=$(git describe --tags --match 'v2.*' --dirty --always 2>/dev/null) || desc=""
    [ -n "$desc" ] || desc="0.0.0-unknown"
    printf '%s' "${desc#v}"
}

# sm_rpm_version sanitises a semver-ish version for the RPM Version field.
# The first '-' (the semver pre-release separator) becomes '~' so a
# pre-release sorts BEFORE the final release (2.0.0~alpha.1 < 2.0.0); every
# remaining '-' becomes '.' so a build N commits past a tag sorts after the
# tag and before the next one.
sm_rpm_version() {
    printf '%s' "$1" | sed 's/-/~/; s/-/./g'
}
