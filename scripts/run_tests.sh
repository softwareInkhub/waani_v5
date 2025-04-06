#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color
YELLOW='\033[1;33m'

echo -e "${YELLOW}Starting API Tests...${NC}"

# Wait for server to be fully up
echo "Waiting for server to be ready..."
max_retries=30
count=0
while ! curl -s http://localhost:8081/devices > /dev/null; do
    sleep 1
    count=$((count + 1))
    if [ $count -eq $max_retries ]; then
        echo -e "${RED}Server did not start within 30 seconds${NC}"
        exit 1
    fi
done

echo -e "${GREEN}Server is ready. Running tests...${NC}"

# Run the tests
cd tests
go test -v ./... 2>&1 | tee ../latest_test_run.log

# Check if tests passed
if [ ${PIPESTATUS[0]} -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
else
    echo -e "${RED}Some tests failed. Check the report for details.${NC}"
fi

# Find the most recent test report
latest_report=$(ls -t test_report_*.json | head -n1)
if [ -n "$latest_report" ]; then
    echo -e "${YELLOW}Latest test report: $latest_report${NC}"
    # You can add more processing here if needed
fi 