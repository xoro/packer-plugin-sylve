NAME=sylve
BINARY=packer-plugin-${NAME}

COUNT ?= 1
TEST=$(shell go list ./...)
HASHICORP_PACKER_PLUGIN_SDK_VERSION=$(shell go list -m github.com/hashicorp/packer-plugin-sdk | cut -d " " -f2)
PLUGIN_FQN=$(shell grep '^module ' go.mod | sed 's/^module //')
VERSION=$(shell sed -n 's/^[[:space:]]*Version = "\([^"]*\)".*/\1/p' version/version.go)

# PLUGIN_INSTALL_FQN is the address passed to "packer plugins install" and used in
# required_plugins.source. It is the short form github.com/<org>/<plugin-name>, not the
# repository name: the repo is packer-plugin-<name> (see HashiCorp docs and e.g.
# github.com/vmware/vmware for module github.com/vmware/packer-plugin-vmware).
PLUGIN_INSTALL_FQN=github.com/xoro/sylve

.PHONY: dev build test testacc generate install-packer-sdc plugin-check dev-freebsd dev-freebsd-root fmt fmt-check lint scan goreleaser-snapshot goreleaser-check

build:
	@go build -o ${BINARY}

fmt:
	@bin/format_code.sh

fmt-check:
	@bin/run_format_checks.sh

lint:
	@bin/run_linter_checks.sh

scan:
	@bin/run_security_scanners.sh

dev:
	go build -ldflags="-X '${PLUGIN_FQN}/version.VersionPrerelease='" -o ${BINARY}
	packer plugins install --path ${BINARY} "${PLUGIN_INSTALL_FQN}"

test:
	@go test -race -count $(COUNT) $(TEST) -timeout=3m

testacc: dev
	@PACKER_ACC=1 go test -count $(COUNT) -v $(TEST) -timeout=120m

install-packer-sdc:
	@go install github.com/hashicorp/packer-plugin-sdk/cmd/packer-sdc@${HASHICORP_PACKER_PLUGIN_SDK_VERSION}

generate: install-packer-sdc
	@go generate ./...

plugin-check: install-packer-sdc build
	@packer-sdc plugin-check ${BINARY}

# Cross-compile for FreeBSD amd64 and install on the remote Sylve host as the current user.
# Requires SYLVE_HOST to be set: make dev-freebsd SYLVE_HOST=user@host
dev-freebsd:
	@if [ -z "$(SYLVE_HOST)" ]; then echo "ERROR: SYLVE_HOST is not set. Usage: make dev-freebsd SYLVE_HOST=user@host"; exit 1; fi
	GOOS=freebsd GOARCH=amd64 go build -ldflags="-X '${PLUGIN_FQN}/version.VersionPrerelease='" -o ${BINARY}_freebsd_amd64
	ssh $(SYLVE_HOST) "mkdir -p ~/.packer.d/plugins/github.com/xoro/sylve"
	scp ${BINARY}_freebsd_amd64 $(SYLVE_HOST):~/.packer.d/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64
	ssh $(SYLVE_HOST) "sha256 -q ~/.packer.d/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64 > ~/.packer.d/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64_SHA256SUM"
	@rm -f ${BINARY}_freebsd_amd64

# Cross-compile for FreeBSD amd64 and install on the remote Sylve host as root.
# Requires SYLVE_HOST to be set: make dev-freebsd-root SYLVE_HOST=user@host
dev-freebsd-root:
	@if [ -z "$(SYLVE_HOST)" ]; then echo "ERROR: SYLVE_HOST is not set. Usage: make dev-freebsd-root SYLVE_HOST=user@host"; exit 1; fi
	GOOS=freebsd GOARCH=amd64 go build -ldflags="-X '${PLUGIN_FQN}/version.VersionPrerelease='" -o ${BINARY}_freebsd_amd64
	scp ${BINARY}_freebsd_amd64 $(SYLVE_HOST):/tmp/${BINARY}_freebsd_amd64_tmp
	ssh -t $(SYLVE_HOST) "PRIV=\$$(command -v doas 2>/dev/null || command -v sudo 2>/dev/null) && \
		\$${PRIV} sh -c 'mkdir -p /root/.packer.d/plugins/github.com/xoro/sylve && \
		mv /tmp/${BINARY}_freebsd_amd64_tmp /root/.packer.d/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64 && \
		sha256 -q /root/.packer.d/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64 > /root/.packer.d/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64_SHA256SUM'"
	@rm -f ${BINARY}_freebsd_amd64

# GoReleaser — requires: go install github.com/goreleaser/goreleaser/v2@latest
# Config: .goreleaser.yml (API_VERSION must stay in sync with packer-plugin-sdk).
# Snapshot: local test build without requiring a git tag.
goreleaser-snapshot:
	goreleaser release --snapshot --clean

# Validate .goreleaser.yml (no build).
goreleaser-check:
	goreleaser check
