#!/bin/bash
set -e

PROCESSOR_DIR="$1"

if [ -z "$PROCESSOR_DIR" ]; then
    echo "Usage: $0 <processor-directory>"
    exit 1
fi

# Convert to absolute path
PROCESSOR_DIR=$(cd "$PROCESSOR_DIR" && pwd)

DESCRIPTION_FILE="$PROCESSOR_DIR/description.yml"

if [ ! -f "$DESCRIPTION_FILE" ]; then
    echo "Error: description.yml not found in $PROCESSOR_DIR"
    exit 1
fi

# Find yq executable
if command -v yq > /dev/null 2>&1; then
    YQ="yq"
elif [ -f "/tmp/yq" ]; then
    YQ="/tmp/yq"
else
    echo "❌ Error: yq not found. Please install yq or ensure it's in PATH"
    exit 1
fi

# Extract processor information
PROCESSOR_NAME=$($YQ eval '.processor.name' "$DESCRIPTION_FILE")
LANGUAGE=$($YQ eval '.processor.language' "$DESCRIPTION_FILE")

echo "Testing processor: $PROCESSOR_NAME"
echo "Directory: $PROCESSOR_DIR"
echo "Language: $LANGUAGE"

# Test based on language
case "$LANGUAGE" in
    Go|go)
        if [ ! -f "$PROCESSOR_DIR/go.mod" ]; then
            echo "❌ Error: No go.mod found in $PROCESSOR_DIR"
            exit 1
        fi

        # Go's package walker skips dirs beginning with '.' or '_', but prune
        # them explicitly so a vendored .venv can never register as a test.
        TEST_COUNT=$(find "$PROCESSOR_DIR" \
            \( -name '.*' -o -name '_*' \) -prune -o \
            -name '*_test.go' -print 2>/dev/null | wc -l)

        if [ "$TEST_COUNT" -eq 0 ]; then
            echo "⚠️  No test files found — nothing to run"
            echo ""
            echo "✅ Processor test validation passed: $PROCESSOR_NAME (no tests)"
            exit 0
        fi

        echo "Found $TEST_COUNT test file(s)"

        cd "$PROCESSOR_DIR"
        if ! go test ./...; then
            echo "❌ Error: Go tests failed"
            exit 1
        fi

        echo "✓ Go tests passed"
        ;;

    TypeScript|typescript|JavaScript|javascript)
        if [ ! -f "$PROCESSOR_DIR/package.json" ]; then
            echo "❌ Error: No package.json found"
            exit 1
        fi

        # Only run when the package actually declares a test script.
        if ! command -v jq > /dev/null 2>&1; then
            echo "⚠️  jq not available — cannot inspect package.json, skipping tests"
            exit 0
        fi

        # Capture jq's status separately from its output: an unparseable
        # package.json must fail loudly rather than look like "no test script".
        if ! TEST_SCRIPT=$(jq -r '.scripts.test // ""' "$PROCESSOR_DIR/package.json" 2>&1); then
            echo "❌ Error: could not parse $PROCESSOR_DIR/package.json"
            echo "$TEST_SCRIPT" | sed 's/^/    /'
            exit 1
        fi

        if [ -z "$(printf '%s' "$TEST_SCRIPT" | tr -d '[:space:]')" ]; then
            echo "⚠️  No 'test' script in package.json — nothing to run"
            echo ""
            echo "✅ Processor test validation passed: $PROCESSOR_NAME (no tests)"
            exit 0
        fi

        cd "$PROCESSOR_DIR"
        if command -v bun > /dev/null 2>&1; then
            RUNNER="bun run"
        elif command -v npm > /dev/null 2>&1; then
            RUNNER="npm run"
        else
            echo "⚠️  Neither bun nor npm available — skipping tests"
            exit 0
        fi

        if ! $RUNNER test; then
            echo "❌ Error: TypeScript tests failed"
            exit 1
        fi

        echo "✓ TypeScript tests passed"
        ;;

    Python|python)
        cd "$PROCESSOR_DIR"

        if ! find . \( -name '.*' \) -prune -o \
            \( -name 'test_*.py' -o -name '*_test.py' \) -print 2>/dev/null | grep -q .; then
            echo "⚠️  No test files found — nothing to run"
            echo ""
            echo "✅ Processor test validation passed: $PROCESSOR_NAME (no tests)"
            exit 0
        fi

        if ! command -v pytest > /dev/null 2>&1; then
            echo "⚠️  pytest not available — skipping tests"
            exit 0
        fi

        if ! pytest -q; then
            echo "❌ Error: Python tests failed"
            exit 1
        fi

        echo "✓ Python tests passed"
        ;;

    Rust|rust)
        if [ ! -f "$PROCESSOR_DIR/Cargo.toml" ]; then
            echo "❌ Error: No Cargo.toml found"
            exit 1
        fi

        cd "$PROCESSOR_DIR"
        if ! cargo test; then
            echo "❌ Error: Cargo tests failed"
            exit 1
        fi

        echo "✓ Rust tests passed"
        ;;

    *)
        echo "⚠️  Warning: Unknown language '$LANGUAGE', skipping tests"
        ;;
esac

echo ""
echo "✅ Processor test validation passed: $PROCESSOR_NAME"
