#!/bin/sh
set -eu

if [ $# -eq 0 ]; then
    echo "usage: $0 <path>..." >&2
    exit 2
fi

find_files() {
    find "$@" \
    -type d \( \
        -name .git \
        -o -name .hg \
        -o -name .svn \
        -o -name .idea \
        -o -name .vscode \
        -o -name .venv \
        -o -name venv \
        -o -name __pycache__ \
        -o -name .pytest_cache \
        -o -name .mypy_cache \
        -o -name .ruff_cache \
        -o -name node_modules \
        -o -name vendor \
        -o -name target \
        -o -name build \
        -o -name dist \
        -o -name bin \
        -o -name tmp \
        -o -name bak \
        -o -name 'bak[0-9]*' \
        -o -name '*.bak' \
        -o -name '*.bak[0-9]*' \
        -o -name '*~' \
    \) -prune \
    -o -type f \
    ! -name '*.bak' \
    ! -name '*~' \
    \( \
        -name '*.go' \
        -o -name 'go.mod' \
        -o -name 'go.sum' \
        -o -name '*.c' \
        -o -name '*.h' \
        -o -name '*.cc' \
        -o -name '*.cpp' \
        -o -name '*.hpp' \
        -o -name '*.rs' \
        -o -name 'Cargo.toml' \
        -o -name 'Cargo.lock' \
        -o -name '*.py' \
        -o -name '*.pyi' \
        -o -name 'pyproject.toml' \
        -o -name 'requirements*.txt' \
        -o -name 'Makefile*' \
        -o -name 'Dockerfile*' \
        -o -name '*.md' \
        -o -name '*.yml' \
        -o -name '*.yaml' \
        -o -name '*.json' \
        -o -name '*.toml' \
        -o -name '*.sh' \
    \) \
    -print \
    | sed 's/^\.\///' \
    | sort
}

set -f
IFS='
'
for f in $(find_files "$@"); do
    printf '\n=== %s ===\n\n' "$f"
    cat -- "$f"
done    
