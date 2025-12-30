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

echo "Building processor: $PROCESSOR_NAME"
echo "Directory: $PROCESSOR_DIR"
echo "Language: $LANGUAGE"

# Build based on language
case "$LANGUAGE" in
    Go|go)
        echo "Building Go processor..."

        # Check for go.mod in processor directory
        if [ ! -f "$PROCESSOR_DIR/go.mod" ]; then
            echo "❌ Error: No go.mod found in $PROCESSOR_DIR"
            exit 1
        fi

        # Find main.go location
        if [ -d "$PROCESSOR_DIR/cmd/$PROCESSOR_NAME" ] && [ -f "$PROCESSOR_DIR/cmd/$PROCESSOR_NAME/main.go" ]; then
            BUILD_DIR="$PROCESSOR_DIR/cmd/$PROCESSOR_NAME"
        elif [ -f "$PROCESSOR_DIR/cmd/main.go" ]; then
            BUILD_DIR="$PROCESSOR_DIR/cmd"
        elif [ -f "$PROCESSOR_DIR/main.go" ]; then
            BUILD_DIR="$PROCESSOR_DIR"
        else
            echo "❌ Error: No main.go found (checked cmd/$PROCESSOR_NAME/main.go, cmd/main.go, main.go)"
            exit 1
        fi

        echo "Build directory: $BUILD_DIR"

        # Build the processor
        cd "$BUILD_DIR"
        if ! go build -o "/tmp/$PROCESSOR_NAME" .; then
            echo "❌ Error: Go build failed"
            exit 1
        fi

        echo "✓ Build successful: /tmp/$PROCESSOR_NAME"

        # Test basic execution (help flag)
        if ! "/tmp/$PROCESSOR_NAME" --help > /dev/null 2>&1; then
            echo "⚠️  Warning: Processor does not respond to --help flag"
        else
            echo "✓ Processor responds to --help"
        fi

        # Clean up binary
        rm -f "/tmp/$PROCESSOR_NAME"
        ;;

    Python|python)
        echo "Validating Python processor..."

        # Check for main file
        if [ ! -f "$PROCESSOR_DIR/main.py" ] && [ ! -f "$PROCESSOR_DIR/__main__.py" ]; then
            echo "❌ Error: No main.py or __main__.py found"
            exit 1
        fi

        # Check for requirements.txt or setup.py
        if [ ! -f "$PROCESSOR_DIR/requirements.txt" ] && [ ! -f "$PROCESSOR_DIR/setup.py" ] && [ ! -f "$PROCESSOR_DIR/pyproject.toml" ]; then
            echo "⚠️  Warning: No requirements.txt, setup.py, or pyproject.toml found"
        fi

        echo "✓ Python processor structure validated"
        ;;

    Rust|rust)
        echo "Building Rust processor..."

        if [ ! -f "$PROCESSOR_DIR/Cargo.toml" ]; then
            echo "❌ Error: No Cargo.toml found"
            exit 1
        fi

        cd "$PROCESSOR_DIR"
        if ! cargo build --release; then
            echo "❌ Error: Cargo build failed"
            exit 1
        fi

        echo "✓ Rust build successful"
        ;;

    *)
        echo "⚠️  Warning: Unknown language '$LANGUAGE', skipping build test"
        ;;
esac

echo ""
echo "✅ Processor build validation passed: $PROCESSOR_NAME"
