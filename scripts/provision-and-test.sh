#!/bin/sh

set -e

if [ $# -eq 0 ]; then
    echo "No arguments provided"
    echo "Usage: $0 <provision-test-arguments>"
    exit 1
fi

provision_args="$@"

echo "Running provision suite"
./provision/provision.test -ginkgo.v -ginkgo.debug -ginkgo.failFast $provision_args

echo "Running main suite"
./tests/tests.test -ginkgo.v -ginkgo.debug -ginkgo.failFast

echo "Running cleanup suite"
./cleanup/cleanup.test -ginkgo.v -ginkgo.debug -ginkgo.failFast
