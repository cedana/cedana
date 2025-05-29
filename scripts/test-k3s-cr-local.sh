#!/bin/bash

# Quick local test runner for k3s pod checkpoint/restore test
# Sets up environment and runs the test

set -euo pipefail

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}🚀 Quick K3s Pod C/R Test${NC}"
echo "=========================="

# Set default environment if not already set
export CEDANA_URL="${CEDANA_URL:-https://ci.cedana.ai}"

# Check if auth token is set
if [ -z "${CEDANA_AUTH_TOKEN:-}" ]; then
    echo -e "${RED}Error: CEDANA_AUTH_TOKEN not set${NC}"
    echo
    echo "Please set your auth token:"
    echo "  export CEDANA_AUTH_TOKEN=\"your_token_here\""
    echo
    echo "Using the provided token:"
    echo "  export CEDANA_AUTH_TOKEN=\"fa4318d1569bc89ac95c1223bbb41719e737640027c87200714204cb813de8a74546a5ec647052bcf19c507ca7013685\""
    exit 1
fi

echo "✓ Environment configured"
echo "  URL: $CEDANA_URL"
echo "  Token: ${CEDANA_AUTH_TOKEN:0:10}..."
echo

# Run the test
echo -e "${GREEN}Running k3s pod checkpoint/restore test...${NC}"
echo

# Use the dedicated script
./scripts/ci/run-k3s-cr-test.sh "$@"

echo
echo -e "${GREEN}🎉 Test completed!${NC}" 