BIN_DIR  := ./bin
LIB_DIR  := $(BIN_DIR)
TMP_DIR  := ./tmp

C_DIR := ./c_lib
C_SRC := $(C_DIR)/calculator.c
C_LIB := $(LIB_DIR)/libcalculator.so

RUST_DIR := ./rust_lib
RUST_SRC := $(RUST_DIR)/src/lib.rs
RUST_LIB := $(LIB_DIR)/libcalculator_rust.so

# source and dest for merge, patch, etc...
SRC   ?= .
DST   ?= 1
MERGE_CODE := sh scripts/merge-code.sh

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

E_RED    := $(if $(COLOR),\033[31m)
E_GREEN  := $(if $(COLOR),\033[32m)
E_YELLOW := $(if $(COLOR),\033[33m)
E_BLUE   := $(if $(COLOR),\033[34m)

# Dark Modern Theme (TrueColor)
E_KEY  := $(if $(COLOR),\033[38;2;86;156;214m)
E_VAR  := $(if $(COLOR),\033[38;2;156;220;254m)
E_FUN  := $(if $(COLOR),\033[38;2;230;230;170m)
E_COM  := $(if $(COLOR),\033[38;2;106;153;85m)

.PHONY: all
all: help

.PHONY: deps generate lint test

deps: ## update deps
	go mod tidy

generate: ## run go generate
	go generate ./...

lint: ## run linters
	CGO_ENABLED=1 $(LINTER) run ./...

test: build-libs ## run tests
	CGO_ENABLED=1 go test --tags=test ./...


.PHONY: build-libs
build-libs: $(C_LIB) $(RUST_LIB) ## build C & Rust test libs

C_HDR := $(wildcard $(C_DIR)/*.h)

$(C_LIB): $(C_SRC) $(C_HDR)
	@mkdir -p $(LIB_DIR)
	gcc -shared -fPIC -O2 -o $@ $<

RUST_TOML  := $(RUST_DIR)/Cargo.toml
RUST_LOCK  := $(RUST_DIR)/Cargo.lock
RUST_BUILT := $(RUST_DIR)/target/release/libcalculator_rust.so

$(RUST_BUILT): $(RUST_SRC) $(RUST_TOML) $(RUST_LOCK)
	cd $(RUST_DIR) && cargo build --release

$(RUST_LIB): $(RUST_BUILT)
	@mkdir -p $(LIB_DIR)
	cp -f $< $@

.PHONY: build-server
build-server: ## build server
	CGO_ENABLED=1 go build -o $(BIN_DIR)/server ./cmd/server

.PHONY: build-generator
build-generator: ## build generator
	CGO_ENABLED=0 go build -o $(BIN_DIR)/generator ./cmd/generator

.PHONY: build ## build all
build: build-libs build-server build-generator

.PHONY: bench
bench: build-libs ## run benchmarks
	CGO_ENABLED=1 go test -bench . -benchmem ./...

.PHONY: clean
clean: ## remove bin and temp files
	-rm -fr *.so $(BIN_DIR) $(LIB_DIR) $(TMP_DIR)
	-cd $(RUST_DIR) && cargo clean


.PHONY: FORCE merge patch help 

FORCE:

merge: ## merge code to file for AI review
	@mkdir -p $(TMP_DIR)
	@$(MERGE_CODE) $(SRC) > "$(TMP_DIR)/$(DST).code"


.NOTPARALLEL: precommit
precommit: deps generate lint test build ## precommit check
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

patch: precommit ## make precommit patch
	@mkdir -p $(TMP_DIR)
	git diff --staged -- $(SRC) > $(TMP_DIR)/$(DST).patch
	@printf "Patch saved to $(TMP_DIR)/$(DST).patch\n"


help: ## show this help
	@printf "$(E_BOLD)Usage:$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) [$(E_VAR)VARIABLE$(E_RSET)=value ...] [$(E_FUN)target$(E_RSET) ...]\n"
	@printf "\n$(E_BOLD)Variables:$(E_RSET)\n"
	@awk 'BEGIN {comment=""} \
		/^[a-zA-Z0-9_-]+[[:space:]]*\?=/ { \
			split($$0, a, /\?=/); \
			gsub(/^[ \t]+|[ \t]+$$/, "", a[1]); \
			gsub(/^[ \t]+|[ \t]+$$/, "", a[2]); \
			if ( prev ~ /^#/ ) { \
				gsub(/^[ \t]+|[ \t]+$$/, "", prev); \
				printf "  $(E_VAR)%-14s$(E_RSET) = %-14s $(E_COM)%s$(E_RSET)\n", a[1], a[2], prev; \
			} else { \
				printf "  $(E_VAR)%-14s$(E_RSET) = %-14s\n", a[1], a[2]; \
			} \
		} \
		{ prev=$$0 }' \
		$(MAKEFILE_LIST)
	@printf "\n$(E_BOLD)Targets:$(E_RSET)\n"
	@awk 'BEGIN {FS = ":.*?## "} \
		/^[a-zA-Z0-9_-]+:.*?## / \
		{printf "  $(E_FUN)%-22s$(E_RSET) - %s\n", $$1, $$2}' \
		$(MAKEFILE_LIST)

	@printf "\n$(E_BOLD)Examples:$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) $(E_FUN)build-libs$(E_RSET)        $(E_COM)# C и Rust библиотеки$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) $(E_FUN)build-server$(E_RSET)      $(E_COM)# Go-сервер$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) $(E_FUN)build-generator$(E_RSET)   $(E_COM)# Go-генератор$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) $(E_FUN)build$(E_RSET)             $(E_COM)# все$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) $(E_FUN)test$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) $(E_FUN)bench$(E_RSET)\n"
	@printf "  $(E_CMD)make$(E_RSET) $(E_FUN)clean$(E_RSET)\n"
