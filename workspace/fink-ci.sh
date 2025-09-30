#!/bin/bash

# Script de test simulé pour home-ci
# Usage: ./fink-ci.sh -b <branch> [autres options]

set -e

# Couleurs pour les logs
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Variables par défaut
BRANCH=""
SIMULATE_FAILURE=false
TEST_DURATION=5
VERBOSE=false

# Fonction d'aide
show_help() {
    echo "Usage: $0 -b <branch> [options]"
    echo ""
    echo "Options:"
    echo "  -b BRANCH      Branch name (required)"
    echo "  -c             Run comprehensive tests (ignored for simulation)"
    echo "  -i OPTION      Additional option (ignored for simulation)"
    echo "  -f             Simulate test failure"
    echo "  -d SECONDS     Test duration in seconds (default: 5)"
    echo "  -v             Verbose output"
    echo "  -h             Show this help"
    exit 0
}

# Parser les arguments
while getopts "b:ci:fd:vh" opt; do
    case $opt in
        b) BRANCH="$OPTARG" ;;
        c) echo "Comprehensive tests enabled" ;;
        i) echo "Additional option: $OPTARG" ;;
        f) SIMULATE_FAILURE=true ;;
        d) TEST_DURATION="$OPTARG" ;;
        v) VERBOSE=true ;;
        h) show_help ;;
        *) echo "Invalid option. Use -h for help."; exit 1 ;;
    esac
done

# Vérifier que la branche est spécifiée
if [ -z "$BRANCH" ]; then
    echo -e "${RED}Error: Branch name is required (-b option)${NC}"
    exit 1
fi

# Fonction de log avec timestamp
log() {
    local level=$1
    local message=$2
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')

    case $level in
        "INFO")  echo -e "${BLUE}[$timestamp] INFO${NC}: $message" ;;
        "WARN")  echo -e "${YELLOW}[$timestamp] WARN${NC}: $message" ;;
        "ERROR") echo -e "${RED}[$timestamp] ERROR${NC}: $message" ;;
        "SUCCESS") echo -e "${GREEN}[$timestamp] SUCCESS${NC}: $message" ;;
    esac
}

# Fonction de test simulé
run_test_phase() {
    local phase_name=$1
    local phase_duration=$2

    log "INFO" "Starting $phase_name..."

    if [ "$VERBOSE" = true ]; then
        for i in $(seq 1 $phase_duration); do
            echo "  [$(date '+%H:%M:%S')] $phase_name step $i/$phase_duration"
            sleep 1
        done
    else
        sleep $phase_duration
    fi

    log "SUCCESS" "$phase_name completed"
}

# Script principal
log "INFO" "Starting tests for branch: $BRANCH"
log "INFO" "Working directory: $(pwd)"
log "INFO" "Git commit: $(git rev-parse HEAD 2>/dev/null || echo 'N/A')"

# Simuler différentes phases de test
run_test_phase "Environment setup" 1
run_test_phase "Unit tests" 2
run_test_phase "Integration tests" $((TEST_DURATION - 3))

# Simuler un échec si demandé
if [ "$SIMULATE_FAILURE" = true ]; then
    log "ERROR" "Test failure simulation activated"
    log "ERROR" "Tests failed for branch $BRANCH"
    exit 1
fi

log "SUCCESS" "All tests passed for branch $BRANCH"
log "INFO" "Test completed successfully"

exit 0