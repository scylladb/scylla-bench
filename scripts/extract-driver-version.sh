#!/bin/bash
set -e

echo "Looking for scylla-gocql driver version..." >&2
DRIVER_VERSION=$(go list -m -json github.com/gocql/gocql | jq -r '.Replace.Version')
DRIVER_REPO=$(go list -m -json github.com/gocql/gocql | jq -r '.Replace.Path')
REPO_OWNER=$(echo "$DRIVER_REPO" | cut -d'/' -f2)
echo "Driver version: $DRIVER_VERSION" >&2
echo "Driver repository: $DRIVER_REPO (owner: $REPO_OWNER)" >&2

DRIVER_COMMIT="unknown"
DRIVER_DATE="unknown"

echo "Trying GitHub API for version info..." >&2
if [ -n "$GITHUB_TOKEN" ]; then
  echo "Using GitHub token for API requests" >&2
  AUTH_HEADER="Authorization: token $GITHUB_TOKEN"
else
  AUTH_HEADER=""
fi

# fetch release information
RELEASE_INFO=$(curl -s -H "$AUTH_HEADER" "https://api.github.com/repos/$REPO_OWNER/gocql/releases/tags/$DRIVER_VERSION" || echo "")
if [ -n "$RELEASE_INFO" ] && [ "$(echo "$RELEASE_INFO" | grep -c "\"message\": \"Not Found\"")" -eq 0 ]; then
  DRIVER_DATE=$(echo "$RELEASE_INFO" | grep '"created_at"' | head -1 | cut -d'"' -f4)
  TAG_INFO=$(curl -s -H "$AUTH_HEADER" "https://api.github.com/repos/$REPO_OWNER/gocql/git/refs/tags/$DRIVER_VERSION" || echo "")
  if [ -n "$TAG_INFO" ] && [ "$(echo "$TAG_INFO" | grep -c "\"message\": \"Not Found\"")" -eq 0 ]; then
    DRIVER_COMMIT=$(echo "$TAG_INFO" | grep '"sha"' | head -1 | cut -d'"' -f4)
  fi
fi

echo "Driver version info:" >&2
echo "  Version: $DRIVER_VERSION" >&2
echo "  Commit:  $DRIVER_COMMIT" >&2
echo "  Date:    $DRIVER_DATE" >&2

echo "DRIVER_VERSION=$DRIVER_VERSION"
echo "DRIVER_COMMIT=$DRIVER_COMMIT"
echo "DRIVER_DATE=$DRIVER_DATE"