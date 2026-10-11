#!/bin/bash
# Check that Retty depends on the selected MyGo fork branch, and that the local
# go.work checkout (if any) matches what go.mod pins. Read-only; exits non-zero
# with a remedy when something drifted.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
FORK=github.com/daodao97/mygo
BRANCH=feat/ios-platform
fail=0

pinned=$(GOWORK=off go list -m -f '{{with .Replace}}{{.Path}} {{.Version}}{{end}}' github.com/egoist/mygo)
path=${pinned%% *}
version=${pinned#* }
if [[ "$path" != "$FORK" || -z "$version" ]]; then
  echo "go.mod must replace github.com/egoist/mygo with $FORK (got: ${pinned:-none})" >&2
  exit 1
fi
commit=${version##*-}
echo "go.mod pins $FORK $version"

# The pinned commit must have been pushed to the explicitly selected branch.
remote_head=$(git ls-remote "https://$FORK.git" "refs/heads/$BRANCH" | cut -c1-12)
if [[ -z "$remote_head" ]]; then
  echo "fork branch $BRANCH was not found" >&2
  exit 1
fi
if [[ "$remote_head" != "$commit" ]]; then
  if [[ -d ../mygo/.git ]] && git -C ../mygo fetch -q "https://$FORK.git" "$BRANCH" && git -C ../mygo merge-base --is-ancestor "$commit" FETCH_HEAD 2>/dev/null; then
    echo "note: pinned $commit is on $BRANCH but behind it ($remote_head); bump with:" >&2
    echo "  GOWORK=off go mod edit -replace github.com/egoist/mygo=$FORK@$BRANCH && GOWORK=off go mod tidy" >&2
  else
    echo "pinned $commit is not the fork's $BRANCH ($remote_head) or an ancestor of it" >&2
    fail=1
  fi
fi

# A go.work checkout is for MyGo development only; it must sit on BRANCH and
# match the pin, or workspace builds silently use different MyGo code.
if [[ -f go.work ]]; then
  dir=$(go list -m -f '{{.Dir}}' github.com/egoist/mygo)
  if [[ "$dir" != "$(GOWORK=off go list -m -f '{{.Dir}}' github.com/egoist/mygo)" ]]; then
    branch=$(git -C "$dir" rev-parse --abbrev-ref HEAD)
    head=$(git -C "$dir" rev-parse --short=12 HEAD)
    echo "go.work uses $dir ($branch @ $head)"
    if [[ "$branch" != "$BRANCH" ]]; then
      echo "go.work MyGo checkout is on '$branch'; switch it to $BRANCH (git -C $dir switch $BRANCH)" >&2
      fail=1
    fi
    if [[ "$head" != "$commit" ]]; then
      echo "go.work MyGo checkout ($head) differs from go.mod ($commit): push and bump go.mod, or check out the pinned commit" >&2
      fail=1
    fi
    if [[ -n "$(git -C "$dir" status --porcelain)" ]]; then
      echo "go.work MyGo checkout has uncommitted changes; GOWORK=off builds will not include them" >&2
    fi
  fi
fi
exit $fail
