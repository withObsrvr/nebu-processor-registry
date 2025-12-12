#!/bin/bash
set -e

PROCESSOR_DIR="$1"

if [ -z "$PROCESSOR_DIR" ]; then
    echo "Usage: $0 <processor-directory>"
    exit 1
fi

if [ ! -d "$PROCESSOR_DIR" ]; then
    echo "Error: Directory $PROCESSOR_DIR does not exist"
    exit 1
fi

DESCRIPTION_FILE="$PROCESSOR_DIR/description.yml"

echo "Validating processor in $PROCESSOR_DIR..."

# Find yq executable
if command -v yq > /dev/null 2>&1; then
    YQ="yq"
elif [ -f "/tmp/yq" ]; then
    YQ="/tmp/yq"
else
    echo "❌ Error: yq not found. Please install yq or ensure it's in PATH"
    exit 1
fi

# Check that description.yml exists
if [ ! -f "$DESCRIPTION_FILE" ]; then
    echo "❌ Error: description.yml not found in $PROCESSOR_DIR"
    exit 1
fi

echo "✓ description.yml exists"

# Validate YAML syntax
if ! $YQ eval '.' "$DESCRIPTION_FILE" > /dev/null 2>&1; then
    echo "❌ Error: description.yml is not valid YAML"
    exit 1
fi

echo "✓ Valid YAML syntax"

# Validate required fields
REQUIRED_FIELDS=(
    ".processor.name"
    ".processor.type"
    ".processor.description"
    ".processor.version"
    ".processor.language"
    ".processor.license"
    ".repo.github"
    ".repo.ref"
)

for field in "${REQUIRED_FIELDS[@]}"; do
    value=$($YQ eval "$field" "$DESCRIPTION_FILE")
    if [ "$value" = "null" ] || [ -z "$value" ]; then
        echo "❌ Error: Required field $field is missing or empty"
        exit 1
    fi
    echo "✓ Required field $field present"
done

# Validate processor type
PROCESSOR_TYPE=$($YQ eval '.processor.type' "$DESCRIPTION_FILE")
if [[ ! "$PROCESSOR_TYPE" =~ ^(origin|transform|sink)$ ]]; then
    echo "❌ Error: processor.type must be one of: origin, transform, sink"
    exit 1
fi

echo "✓ Valid processor type: $PROCESSOR_TYPE"

# Validate version format (semver)
VERSION=$($YQ eval '.processor.version' "$DESCRIPTION_FILE")
if ! echo "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$'; then
    echo "⚠️  Warning: Version '$VERSION' does not follow semver format (e.g., 1.0.0)"
fi

# Validate GitHub repository format
GITHUB_REPO=$($YQ eval '.repo.github' "$DESCRIPTION_FILE")
if ! echo "$GITHUB_REPO" | grep -Eq '^[a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+$'; then
    echo "❌ Error: repo.github must be in format 'username/repository'"
    exit 1
fi

echo "✓ Valid GitHub repository: $GITHUB_REPO"

# Validate maintainers list
MAINTAINERS_COUNT=$($YQ eval '.processor.maintainers | length' "$DESCRIPTION_FILE")
if [ "$MAINTAINERS_COUNT" -eq 0 ]; then
    echo "❌ Error: At least one maintainer must be specified"
    exit 1
fi

echo "✓ Maintainers specified: $MAINTAINERS_COUNT"

# Check documentation sections
DOCS_SECTIONS=(".docs.quick_start" ".docs.examples")
for section in "${DOCS_SECTIONS[@]}"; do
    value=$($YQ eval "$section" "$DESCRIPTION_FILE")
    if [ "$value" = "null" ] || [ -z "$value" ]; then
        echo "⚠️  Warning: Recommended section $section is missing"
    else
        echo "✓ Documentation section $section present"
    fi
done

echo ""
echo "✅ Processor validation passed: $PROCESSOR_DIR"
