#!/bin/bash
# (c) Copyright 2019 Capsule 8, Inc (capsule8.io)
#
# Run tests for the Happyholic1203/capsule8 repository

CAPSULE8_DIR="$(git rev-parse --show-toplevel)"
WORK_DIR="/go/src/github.com/Happyholic1203/capsule8"

docker run -it --rm -e TEST_ARGS="$TEST_ARGS" -v "$CAPSULE8_DIR:$WORK_DIR" \
	-w $WORK_DIR golang:1.11.4 /bin/bash -c "make test"
