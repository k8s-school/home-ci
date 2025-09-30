#!/bin/bash

# Script de test simulé pour home-ci
# Usage: ./fink-ci.sh -b <branch> [autres options]

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$(dirname "$DIR")"

echo "Building project in $PROJECT_DIR"
go build $PROJECT_DIR


echo "Running tests..."
$PROJECT_DIR/home-ci -c $DIR/config.yaml -v 5
