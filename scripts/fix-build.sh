#!/bin/bash
# Script to fix build issues

set -e

cd "$(dirname "$0")/.."

echo "Fixing storage.New calls in test files..."
find . -type f -name "*_test.go" -exec sed -i 's/storage\.New(storageConfig, logger)/storage.NewManager(storageConfig, logger)/g' {} \;

echo "Fixing manager.Start() calls to include context..."
find . -type f -name "*_test.go" -exec sed -i 's/err = manager\.Start()/ctx := context.Background()\n\terr = manager.Start(ctx)/g' {} \;

echo "Fixing StoreMediaMetadata to AddMediaMetadata..."
find . -type f -name "*.go" -exec sed -i 's/StoreMediaMetadata/AddMediaMetadata/g' {} \;

echo "Running go mod tidy..."
go mod tidy

echo "Building..."
go build ./cmd/go-jf-watch

echo "Done!"
