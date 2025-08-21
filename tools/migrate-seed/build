#!/bin/bash

# Build the seed migration tool

echo "Building migrate-seed tool..."
go build -o bin/migrate-seed .

if [ $? -eq 0 ]; then
    echo "✅ Successfully built migrate-seed tool at bin/migrate-seed"
    echo ""
    echo "Usage:"
    echo "  ./bin/migrate-seed -input data/seed.json -output data/seed-migrated.json"
    echo ""
else
    echo "❌ Failed to build migrate-seed tool"
    exit 1
fi