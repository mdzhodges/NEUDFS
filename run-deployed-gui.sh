#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

exec "$ROOT/run.sh" "$@"
