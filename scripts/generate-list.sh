#!/bin/bash
set -e

OUTPUT_FILE="PROCESSORS.md"

# Find yq executable
if command -v yq > /dev/null 2>&1; then
    YQ="yq"
elif [ -f "/tmp/yq" ]; then
    YQ="/tmp/yq"
else
    echo "❌ Error: yq not found. Please install yq or ensure it's in PATH"
    exit 1
fi

echo "Generating processor list..."

cat > "$OUTPUT_FILE" << 'EOF'
# Available Processors

This is an auto-generated list of all processors in the nebu community registry.

EOF

# Count processors
TOTAL=0

# Add sections for each processor type
for TYPE in origin transform sink; do
    echo "" >> "$OUTPUT_FILE"
    echo "## ${TYPE^} Processors" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"

    FOUND=0

    # Find all description.yml files
    for desc_file in processors/*/description.yml; do
        if [ ! -f "$desc_file" ]; then
            continue
        fi

        # Check if this is the right type
        PROC_TYPE=$($YQ eval '.processor.type' "$desc_file")
        if [ "$PROC_TYPE" != "$TYPE" ]; then
            continue
        fi

        FOUND=1
        TOTAL=$((TOTAL + 1))

        # Extract processor details
        NAME=$($YQ eval '.processor.name' "$desc_file")
        DESC=$($YQ eval '.processor.description' "$desc_file")
        VERSION=$($YQ eval '.processor.version' "$desc_file")
        LANGUAGE=$($YQ eval '.processor.language' "$desc_file")
        LICENSE=$($YQ eval '.processor.license' "$desc_file")
        GITHUB=$($YQ eval '.repo.github' "$desc_file")

        # Add to output
        echo "### $NAME" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
        echo "$DESC" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
        echo "- **Version**: $VERSION" >> "$OUTPUT_FILE"
        echo "- **Language**: $LANGUAGE" >> "$OUTPUT_FILE"
        echo "- **License**: $LICENSE" >> "$OUTPUT_FILE"
        echo "- **Repository**: [github.com/$GITHUB](https://github.com/$GITHUB)" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"

        # Add installation and usage
        echo "\`\`\`bash" >> "$OUTPUT_FILE"
        echo "# Install" >> "$OUTPUT_FILE"
        echo "nebu install $NAME" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"

        # Type-specific usage examples
        case "$TYPE" in
            origin)
                echo "# Use as origin" >> "$OUTPUT_FILE"
                echo "$NAME --start-ledger 60200000 --end-ledger 60200100 | jq" >> "$OUTPUT_FILE"
                ;;
            transform)
                echo "# Use in pipeline" >> "$OUTPUT_FILE"
                echo "token-transfer | $NAME | json-file-sink" >> "$OUTPUT_FILE"
                ;;
            sink)
                echo "# Use as sink" >> "$OUTPUT_FILE"
                echo "token-transfer | $NAME" >> "$OUTPUT_FILE"
                ;;
        esac

        echo "\`\`\`" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
    done

    if [ $FOUND -eq 0 ]; then
        echo "*No $TYPE processors available yet.*" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
    fi
done

echo "" >> "$OUTPUT_FILE"
echo "---" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"
echo "*Total processors: $TOTAL*" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"
echo "*Last updated: $(date -u '+%Y-%m-%d %H:%M:%S UTC')*" >> "$OUTPUT_FILE"

echo "✅ Generated processor list: $OUTPUT_FILE ($TOTAL processors)"
