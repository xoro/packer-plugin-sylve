NAME=sylve
BINARY=packer-plugin-${NAME}

COUNT ?= 1
TEST != go list ./...
HASHICORP_PACKER_PLUGIN_SDK_VERSION != go list -m github.com/hashicorp/packer-plugin-sdk | cut -d " " -f2
PLUGIN_FQN != grep -E '^module' go.mod | sed 's/module[[:space:]]*//' 
VERSION != grep '^\tVersion' version/version.go | sed 's/.*"\(.*\)"/\1/'

# PLUGIN_INSTALL_FQN is the address passed to "packer plugins install" and used in
# required_plugins.source. It is the short form github.com/<org>/<plugin-name>, not the
# repository name: the repo is packer-plugin-<name> (see HashiCorp docs and e.g.
# github.com/vmware/vmware for module github.com/vmware/packer-plugin-vmware).
PLUGIN_INSTALL_FQN=github.com/xoro/sylve

.PHONY: dev build test testacc generate install-packer-sdc plugin-check deploy-freebsd fmt fmt-check lint scan goreleaser-snapshot goreleaser-check

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

# Cross-compile for FreeBSD amd64 and install on remote Sylve host.
# Usage: make deploy-freebsd HOST=user@host
deploy-freebsd:
	@if [ -z "$(HOST)" ]; then echo "Usage: make deploy-freebsd HOST=user@host"; exit 1; fi
	GOOS=freebsd GOARCH=amd64 go build -ldflags="-X '${PLUGIN_FQN}/version.VersionPrerelease=dev'" -o ${BINARY}_freebsd_amd64
	scp ${BINARY}_freebsd_amd64 $(HOST):/tmp/
	ssh $(HOST) "mkdir -p ~/.config/packer/plugins/github.com/xoro/sylve/ && \
		mv /tmp/${BINARY}_freebsd_amd64 \
		   ~/.config/packer/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64 && \
		chmod 0755 ~/.config/packer/plugins/github.com/xoro/sylve/${BINARY}_v${VERSION}_x5.0_freebsd_amd64"
	@rm -f ${BINARY}_freebsd_amd64

# GoReleaser — requires: go install github.com/goreleaser/goreleaser/v2@latest
# Config: .goreleaser.yml (API_VERSION must stay in sync with packer-plugin-sdk).
# Snapshot: local test build without requiring a git tag.
goreleaser-snapshot:
	goreleaser release --snapshot --clean

# Validate .goreleaser.yml (no build).
goreleaser-check:
	goreleaser check
