BIN_DIR = ./bin
LIB_DIR = $(BIN_DIR)
TMP_DIR = ./tmp

# source and dest for merge, patch, etc...
SRC   ?= .
DST   ?= 1

# tip: to disable all linters can set LINTER=true
LINTER ?= golangci-lint

EMPTY :=
SPACE := $(EMPTY) $(EMPTY)
COMMA := ,

# Escape-последовательности терминала
CHECK_COLOR := $(shell [ -n "$(MAKE_TERMOUT)" ] && echo "$(TERM)" | grep -EE "color|256|xterm" >/dev/null && echo "yes")
# use color output (yes|no)
COLOR ?= $(CHECK_COLOR)

E_RSET := $(if $(COLOR),\033[0m)
E_BOLD := $(if $(COLOR),\033[1m)
E_CMD  :=

E_RED    := \033[31m
E_GREEN  := \033[32m
E_YELLOW := \033[33m
E_BLUE   := \033[34m

# Dark Modern Theme (TrueColor)
E_KEY  := $(if $(COLOR),\033[38;2;86;156;214m)
E_VAR  := $(if $(COLOR),\033[38;2;156;220;254m)
E_FUN  := $(if $(COLOR),\033[38;2;230;230;170m)
E_COM  := $(if $(COLOR),\033[38;2;106;153;85m)

.PHONY: all
all: help

deps: ## update deps
	go mod tidy
	if test -d tests; then cd tests && go mod tidy; fi 

generate:
	go generate ./...

lint: ## run linters
	CGO_ENABLED=1 $(LINTER) run ./...

test: ## run tests
	CGO_ENABLED=1 go test --tags=test ./...


.PHONY: build-libs
build-libs: ## build C & Rust test libs
	gcc -shared -fPIC -O2 -o $(LIB_DIR)/libcalculator.so c_lib/calculator.c
	cd rust_lib && cargo build --release
	cp -f rust_lib/target/release/libcalculator_rust.so $(LIB_DIR)

.PHONY: build-server
build-server: ## build server
	CGO_ENABLED=1 go build -o $(BIN_DIR)/server ./cmd/server

.PHONY: build-generator
build-generator: ## build generator
	CGO_ENABLED=0 go build -o $(BIN_DIR)/generator ./cmd/generator

.PHONY: build
build: build-server build-generator build-libs

.PHONY: clean
clean: ## remove bin and temp files
	-rm -fr *.so $(BIN_DIR) $(LIB_DIR) $(TMP_DIR)


.PHONY: FORCE merge patch help 

FORCE:

merge: ## merge code to file for AI review
	@mkdir -p $(TMP_DIR)
	@find $(SRC) \
		! -path 'tmp/*' \
		! -path 'bak/*' \
		! -path 'target/*' \
		-type f \
		\( \
			-name '*.go' \
			-o -name 'go.mod' \
			-o -name '*.py' \
			-o -name '*.h'  \
			-o -name '*.rs' \
			-o -name '*.toml' \
			-o -name '*.sh' \
			-o -name '*.md' \
			-o -name '*.y*ml' \
			-o -name 'Makefile*' \
			-o -name 'Dockerfile*' \
		\) \
		-exec sh -c 'printf "\n=== {} ===\n\n"; cat {}' ';' \
		> $(TMP_DIR)/$(DST).code


.NOTPARALLEL: patch
patch: deps generate lint test build ## make precommit patch
	@mkdir -p $(TMP_DIR)
	
	@(set -e; \
	staged_list="$(TMP_DIR)/staged_list.$$$$"; \
	unstaged_list="$(TMP_DIR)/unstaged_list.$$$$"; \
	git diff --staged --name-only -- $(SRC) > "$$staged_list"; \
	git diff --name-only -- $(SRC) > "$$unstaged_list"; \
	intersection=$$(grep -Fxf "$$staged_list" "$$unstaged_list" || true); \
	rm -f "$$staged_list" "$$unstaged_list"; \
	if [ -n "$$intersection" ]; then \
		printf "\n" >&2; \
		printf "$(E_BOLD)WARNING:$(E_RSET) the following files have changes not staged for commit:\n" >&2; \
		printf "  (use \"git add <file>...\" to update what will be committed)\n" >&2; \
		printf "$(E_RED)%s$(E_RSET)\n" $$intersection | sed 's/^/        /' >&2; \
		printf "\n" >&2; \
	fi)
	
	git diff --staged -- $(SRC) > $(TMP_DIR)/$(DST).patch
	@printf "Patch saved to $(TMP_DIR)/$(DST).patch\n"


.PHONY: help
help: ## show this help
	@printf "Usage: make [target] [VAR=value]\n\n"
	@printf "Variables:\n"
	@awk 'BEGIN {comment=""} \
		/^[a-zA-Z0-9_-]+[[:space:]]*[?]=/ { \
			split($$0, a, /[[:space:]]*[?]=[[:space:]]*/); \
			if ( prev ~ /^#/ ) { \
				printf "  %-14s = %-20s %s\n", a[1], a[2], prev; \
			} else { \
				printf "  %-14s = %-20s\n", a[1], a[2]; \
			} \
		} \
		{ prev=$$0 }' $(MAKEFILE_LIST)
	@printf "\nTargets:\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "  %-14s - %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	