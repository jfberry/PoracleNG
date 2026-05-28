#!/usr/bin/env bash
# Emit the Go -ldflags "-X" assignments that stamp version metadata into the
# processor binary from the local git checkout. Used by the Makefile and
# start.sh.
#
# The Docker build can't use this (its build context has no .git); it injects
# the equivalent values via build args (VERSION_COMMIT / VERSION_BRANCH /
# VERSION_DATE) instead. Keep the variable names here in sync with the
# Dockerfile and processor/version.go.
set -euo pipefail

pkg="github.com/pokemon/poracleng/processor"

commit="$(git rev-parse --short=8 HEAD 2>/dev/null || echo unknown)"
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
	commit="${commit}-dirty"
fi

branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '')"
if [ "$branch" = "HEAD" ]; then
	branch="" # detached HEAD: no meaningful branch name
fi

date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

printf -- '-X %s.Commit=%s -X %s.Branch=%s -X %s.Date=%s' \
	"$pkg" "$commit" "$pkg" "$branch" "$pkg" "$date"
