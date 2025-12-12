#!/bin/bash
set -e

PROCESSOR_DIR="$1"

if [ -z "$PROCESSOR_DIR" ]; then
    echo "Usage: $0 <processor-directory>"
    exit 1
fi

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

# Extract repository information
GITHUB_REPO=$($YQ eval '.repo.github' "$DESCRIPTION_FILE")
GIT_REF=$($YQ eval '.repo.ref' "$DESCRIPTION_FILE")
PROCESSOR_NAME=$($YQ eval '.processor.name' "$DESCRIPTION_FILE")
LANGUAGE=$($YQ eval '.processor.language' "$DESCRIPTION_FILE")

echo "Building processor: $PROCESSOR_NAME"
echo "Repository: https://github.com/$GITHUB_REPO"
echo "Reference: $GIT_REF"
echo "Language: $LANGUAGE"

# Create temporary directory for cloning
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Clone the repository
echo "Cloning repository..."
git clone --quiet "https://github.com/$GITHUB_REPO" "$TEMP_DIR/repo"
cd "$TEMP_DIR/repo"

# Checkout specific ref
echo "Checking out $GIT_REF..."
git checkout --quiet "$GIT_REF"

# Build based on language
case "$LANGUAGE" in
    Go|go)
        echo "Building Go processor..."

        # Check for cmd/main.go or main.go
        if [ -f "cmd/main.go" ]; then
            cd cmd
        elif [ ! -f "main.go" ]; then
            echo "❌ Error: No main.go found in repository"
            exit 1
        fi

        # Try to build
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
        ;;

    Python|python)
        echo "Validating Python processor..."

        # Check for main file
        if [ ! -f "main.py" ] && [ ! -f "__main__.py" ]; then
            echo "❌ Error: No main.py or __main__.py found"
            exit 1
        fi

        # Check for requirements.txt or setup.py
        if [ ! -f "requirements.txt" ] && [ ! -f "setup.py" ] && [ ! -f "pyproject.toml" ]; then
            echo "⚠️  Warning: No requirements.txt, setup.py, or pyproject.toml found"
        fi

        echo "✓ Python processor structure validated"
        ;;

    Rust|rust)
        echo "Building Rust processor..."

        if [ ! -f "Cargo.toml" ]; then
            echo "❌ Error: No Cargo.toml found"
            exit 1
        fi

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
