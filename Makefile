# prledger tasks. Releases are cut from this machine; there are no workflows.
#
#   make check                         gofmt, vet and race tests
#   make release-dry VERSION=v0.1.0    everything except tag, push and publish
#   make release VERSION=v0.1.0        tag, push and publish on GitHub
#
# Publishing goes through gh. To use another gh account:
#   make release VERSION=v0.1.0 GH="env GH_CONFIG_DIR=$HOME/.config/gh-personal gh"

VERSION ?=
GH      ?= gh
DIST    := dist
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
# owner/name from the origin remote; works with SSH host aliases.
REPO     = $(shell git remote get-url origin | sed -E 's|\.git$$||; s|.*[:/]([^/]+/[^/]+)$$|\1|')

# Release steps must run in order.
.NOTPARALLEL:
.PHONY: check build clean version guard dist verify release-dry release

check:
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go test -race ./...

build:
	go build -o prledger .

clean:
	rm -rf $(DIST) prledger

version:
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' \
		|| { echo "set VERSION=vX.Y.Z (or vX.Y.Z-rc.1), got '$(VERSION)'"; exit 2; }

# Refuse to release uncommitted work or reuse a tag.
guard: version
	@test -z "$$(git status --porcelain)" || { echo "the working tree has uncommitted changes; commit them first"; exit 1; }
	@! git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null || { echo "tag $(VERSION) already exists locally"; exit 1; }
	@test -z "$$(git ls-remote --tags origin refs/tags/$(VERSION))" || { echo "tag $(VERSION) already exists on origin"; exit 1; }
	@[ "$$(git rev-parse --abbrev-ref HEAD)" = main ] || echo "warning: releasing from $$(git rev-parse --abbrev-ref HEAD), not main"

# Cross-build archives with the version baked in, plus checksums.
dist: version
	rm -rf $(DIST) && mkdir -p $(DIST)
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; name=prledger_$(VERSION:v%=%)_$${os}_$${arch}; exe=prledger; \
		[ $$os = windows ] && exe=prledger.exe; \
		mkdir -p $(DIST)/$$name \
		&& CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(DIST)/$$name/$$exe . \
		&& cp LICENSE README.md $(DIST)/$$name/ \
		&& if [ $$os = windows ]; then (cd $(DIST) && zip -qr $$name.zip $$name); else tar -C $(DIST) -czf $(DIST)/$$name.tar.gz $$name; fi \
		&& rm -rf $(DIST)/$$name && echo "  $(DIST)/$$name" || exit 1; \
	done
	cd $(DIST) && shasum -a 256 *.tar.gz *.zip > checksums.txt

# The archive for this machine must report the version being released.
verify: dist
	@host=prledger_$(VERSION:v%=%)_$$(go env GOOS)_$$(go env GOARCH); tmp=$$(mktemp -d); \
		tar -C $$tmp -xzf $(DIST)/$$host.tar.gz; got=$$($$tmp/$$host/prledger version); rm -rf $$tmp; \
		[ "$$got" = "prledger $(VERSION)" ] || { echo "built binary says '$$got', want 'prledger $(VERSION)'"; exit 1; }; \
		echo "  $$got"

release-dry: guard check verify
	@echo "Dry run. A real release would now run:"
	@echo "  git tag -a $(VERSION) -m \"prledger $(VERSION)\""
	@echo "  git push origin $(VERSION)"
	@echo "  $(GH) release create $(VERSION) --repo $(REPO) --verify-tag --generate-notes --title $(VERSION) $(DIST)/*"
	@ls -1 $(DIST)

release: guard check verify
	git tag -a $(VERSION) -m "prledger $(VERSION)"
	git push origin $(VERSION)
	$(GH) release create $(VERSION) --repo $(REPO) --verify-tag --generate-notes --title $(VERSION) $(DIST)/*
	@echo "Released $(VERSION). Install with: go install github.com/$(REPO)@$(VERSION)"
