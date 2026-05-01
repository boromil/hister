#!/bin/sh

BASE_DIR="$(dirname -- "`readlink -f -- "$0"`")"
ACTION="$1"
[ -z "$ACTION" ] || shift

EXT_CHROME_ZIP='hister_ext_chrome.zip'
EXT_FF_ZIP='hister_ext_ff.zip'
EXT_SRC_ZIP='hister_ext_src.zip'

cd -- "$BASE_DIR"
set -e


help() {
	[ -z "$1" ] || printf 'Error: %s\n' "$1"
	echo "hister manage.sh help

Commands
========
help                 - Display help

Dependencies
------------------
install_js_deps      - Install or install frontend dependencies (required only for development)

Tests
-----
run_unit_tests       - Run unit tests

 Build
 -----
 build                - Build main hister application
 build_addon          - Build addon
 build_addon_artifact - Build addon artifacts to distribute to addon stores
 build_website        - Build website

 ========

 Execute 'go run hister.go' or 'go build && ./hister' for application related actions
 "
	[ -z "$1" ] && exit 0 || exit 1
}

check_bun() {
    if ! command -v bun >/dev/null 2>&1; then
        echo "!!!!!Error: BUN IS NOT INSTALLED!!!!! Please install bun from https://bun.sh"
        exit 1
    fi
}

install_js_deps() {
    check_bun
    bun install
}

run_unit_tests() {
    go test ./...
}

build() {
    check_bun
    go generate && go build
}

build_addon() {
    check_bun
    echo "[!] Warning: The default manifest.json is for chrome browsers, overwrite it with manifest_ff.json for firefox"
    bun run --cwd webui/ext build
}

build_website() {
    check_bun
    bun run --cwd webui/website build
}

build_addon_artifact() {
    build_addon
    [ -e "webui/ext/$EXT_SRC_ZIP" ] && rm "webui/ext/$EXT_SRC_ZIP" || :
    [ -e "webui/ext/$EXT_CHROME_ZIP" ] && rm "webui/ext/$EXT_CHROME_ZIP" || :
    [ -e "webui/ext/$EXT_FF_ZIP" ] && rm "webui/ext/$EXT_FF_ZIP" || :
    cd webui/ext
    zip -r "$EXT_SRC_ZIP" src tsconfig.json package* webpack.config.js
    cd dist
    zip "../$EXT_CHROME_ZIP" ./* assets/* assets/icons/*
    cd ../
    cp "$EXT_CHROME_ZIP" "$EXT_FF_ZIP"
    zip -d "$EXT_CHROME_ZIP" manifest_ff.json
    zip -d "$EXT_FF_ZIP" manifest.json
    printf "@ manifest_ff.json\n@=manifest.json\n" | zipnote -w "$EXT_FF_ZIP"
}

[ "$(command -V "$ACTION" | grep ' function$')" = "" ] \
	&& help "action not found" \
	|| "$ACTION" "$@"
