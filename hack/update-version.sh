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

# Function to update version and release labels in a Dockerfile
update_dockerfile_version() {
    local file=$1
    local version=$2

    echo "Updating $file..."
    sed -i "s/release=\"[^\"]*\"/release=\"$version\"/g" "$file"
    sed -i "s/version=\"[^\"]*\"/version=\"$version\"/g" "$file"
}

echo "Bumping version to $VERSION in Dockerfiles..."

# Update all Dockerfiles
update_dockerfile_version "Dockerfile" "$VERSION"

echo ""
echo "Version update completed"
