#!/usr/bin/env bash
# shellcheck shell=bash
set -e

cd "$(dirname -- "$0")/.." || exit

bun install
bun run --cwd webui/app build

rm -rf server/static/app
mkdir -p server/static
cp -r webui/app/build server/static/app
