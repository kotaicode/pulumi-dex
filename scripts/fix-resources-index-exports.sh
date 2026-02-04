#!/bin/bash
# Post-process resources/index.ts to fix Args exports for isolatedModules
# This fixes TS1205 errors by converting Args exports to export type

set -e

RESOURCES_INDEX_FILE="sdk/typescript/nodejs/resources/index.ts"

if [ ! -f "$RESOURCES_INDEX_FILE" ]; then
    echo "Error: $RESOURCES_INDEX_FILE not found"
    exit 1
fi

echo "Fixing Args exports in resources/index.ts for isolatedModules..."

# Use sed to replace "export { XArgs }" with "export type { XArgs }"
# This fixes TS1205 errors when isolatedModules is enabled
sed -i.bak 's/export { \([A-Za-z]*Args\) }/export type { \1 }/g' "$RESOURCES_INDEX_FILE"
rm -f "${RESOURCES_INDEX_FILE}.bak"
echo "✓ Fixed Args exports in resources/index.ts"

echo "✓ Resources index exports fixed"
