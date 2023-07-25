#!/bin/bash

cd "$(dirname "$0")" || exit 1
./mca-cleanup.sh
./mcv-cleanup.sh