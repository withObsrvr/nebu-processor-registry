#!/usr/bin/env bash
set -euo pipefail

OUTPUT_FILE="PROCESSORS.md"

extract_scalar() {
    local section="$1"
    local key="$2"
    local file="$3"

    awk -v section="$section" -v key="$key" '
        function ltrim(s) { sub(/^[[:space:]]+/, "", s); return s }
        function rtrim(s) { sub(/[[:space:]]+$/, "", s); return s }
        function trim(s)  { return rtrim(ltrim(s)) }
        BEGIN { in_section = 0 }
        $0 ~ "^" section ":" { in_section = 1; next }
        in_section && $0 ~ /^[^[:space:]]/ { exit }
        in_section {
            pattern = "^[[:space:]]+" key ":[[:space:]]*(.*)$"
            if (match($0, pattern, m)) {
                val = trim(m[1])
                gsub(/^"|"$/, "", val)
                gsub(/^'\''|'\''$/, "", val)
                print val
                exit
            }
        }
    ' "$file"
}

echo "Generating processor list..."

cat > "$OUTPUT_FILE" << 'EOF'
# Available Processors

This is an auto-generated list of all processors in the nebu community registry.
EOF

TOTAL=0

for TYPE in origin transform sink; do
    echo "" >> "$OUTPUT_FILE"
    echo "## ${TYPE^} Processors" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"

    FOUND=0

    for desc_file in processors/*/description.yml; do
        [[ -f "$desc_file" ]] || continue

        PROC_TYPE="$(extract_scalar processor type "$desc_file")"
        [[ "$PROC_TYPE" == "$TYPE" ]] || continue

        FOUND=1
        TOTAL=$((TOTAL + 1))

        NAME="$(extract_scalar processor name "$desc_file")"
        DESC="$(extract_scalar processor description "$desc_file")"
        VERSION="$(extract_scalar processor version "$desc_file")"
        LANGUAGE="$(extract_scalar processor language "$desc_file")"
        LICENSE="$(extract_scalar processor license "$desc_file")"
        GITHUB="$(extract_scalar repo github "$desc_file")"
        SCHEMA="$(extract_scalar schema identifier "$desc_file" || true)"

        echo "### $NAME" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
        echo "$DESC" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
        [[ -n "$VERSION" ]] && echo "- **Version**: $VERSION" >> "$OUTPUT_FILE"
        [[ -n "$LANGUAGE" ]] && echo "- **Language**: $LANGUAGE" >> "$OUTPUT_FILE"
        [[ -n "$LICENSE" ]] && echo "- **License**: $LICENSE" >> "$OUTPUT_FILE"
        [[ -n "$SCHEMA" ]] && printf -- '- **Schema**: `%s`\n' "$SCHEMA" >> "$OUTPUT_FILE"
        [[ -n "$GITHUB" ]] && echo "- **Repository**: [github.com/$GITHUB](https://github.com/$GITHUB)" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"

        echo '```bash' >> "$OUTPUT_FILE"
        echo "# Install" >> "$OUTPUT_FILE"
        echo "nebu install $NAME" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"

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

        echo '```' >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
    done

    if [[ $FOUND -eq 0 ]]; then
        echo "*No $TYPE processors available yet.*" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
    fi
done

echo "" >> "$OUTPUT_FILE"
echo "---" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"
echo "*Total processors: $TOTAL*" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"
echo "*Last updated: $(date -u '+%Y-%m-%d')*" >> "$OUTPUT_FILE"

echo "✅ Generated processor list: $OUTPUT_FILE ($TOTAL processors)"
