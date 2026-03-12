#!/bin/bash

set -e

VERSION=$1

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 5.2.3"
    exit 1
fi

# Validate version format (basic check for semantic versioning)
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Version must be in format X.Y.Z (e.g., 5.2.3)"
    exit 1
fi

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "$REPO_ROOT"

echo "Verifying changes..."
FAILED=0

# Verify Dockerfiles
for file in Dockerfile ; do
    if grep -q "release=\"$VERSION\"" "$file" && grep -q "version=\"$VERSION\"" "$file"; then
        echo "✓ $file: version updated successfully"
    else
        echo "✗ $file: version update failed (expected: $VERSION)"
        FAILED=1
    fi
done

if [ $FAILED -eq 1 ]; then
    echo ""
    echo "Error: Version verification failed for one or more files"
    exit 1
fi

echo ""
echo "Version successfully verified in all files"
