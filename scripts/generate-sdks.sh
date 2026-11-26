#!/bin/bash
# Generate the TypeScript, Go and Python SDKs from schema.json and apply the
# post-processing the published packages need. Every step is required: any
# failure stops the script with a nonzero exit instead of leaving a partial SDK
# behind a success message.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

[ -s schema.json ] || { echo "✗ schema.json is missing or empty; run make generate-schema" >&2; exit 1; }

# The TypeScript README is maintained by hand; codegen overwrites it with a stub.
readme_backup=$(mktemp)
trap 'rm -f "$readme_backup"' EXIT
has_readme=false
if [ -s sdk/typescript/nodejs/README.md ]; then
    cp sdk/typescript/nodejs/README.md "$readme_backup"
    has_readme=true
fi

pulumi package gen-sdk schema.json --language typescript --out sdk/typescript
pulumi package gen-sdk schema.json --language go --out sdk/go
pulumi package gen-sdk schema.json --language python --out sdk/python

# Go codegen writes sdk/go/go/dex, but the module (sdk/go.mod) expects sdk/go/dex.
# Replace the old package only once the new one exists.
[ -d sdk/go/go/dex ] || { echo "✗ Go codegen did not produce sdk/go/go/dex" >&2; exit 1; }
rm -rf sdk/go/dex
mv sdk/go/go/dex sdk/go/dex
# codegen also writes sdk/go/go/.gitattributes (marks files as generated); keep it one level up
if [ -f sdk/go/go/.gitattributes ]; then
    mv -f sdk/go/go/.gitattributes sdk/go/.gitattributes
fi
rmdir sdk/go/go
# sdk/go.mod is maintained in the repository; codegen does not write it.
[ -f sdk/go.mod ] || { echo "✗ sdk/go.mod is missing" >&2; exit 1; }

if [ "$has_readme" = true ]; then
    cp "$readme_backup" sdk/typescript/nodejs/README.md
fi
# Python codegen clears its output directory and writes no README; publish the repository README.
cp README.md sdk/python/python/README.md

# TypeScript export fixes for isolatedModules (idempotent; they fail only on real errors)
./scripts/fix-typescript-exports.sh
./scripts/fix-resources-index-exports.sh

echo "✓ SDKs generated in sdk/"
