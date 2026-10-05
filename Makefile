.PHONY: check build release-dry release

# Everything a change must pass before it is committed or released.
check:
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go test -race ./...

build:
	go build -o prledger .

# make release-dry VERSION=v0.1.0
release-dry:
	@test -n "$(VERSION)" || (echo "usage: make release-dry VERSION=vX.Y.Z" && exit 2)
	bash scripts/release.sh $(VERSION) --dry-run

# make release VERSION=v0.1.0
release:
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=vX.Y.Z" && exit 2)
	bash scripts/release.sh $(VERSION)
