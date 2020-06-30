release:
	goreleaser release --rm-dist

snapshot:
	goreleaser release --rm-dist --snapshot --skip-publish --skip-sign --skip-validate

.PHONY: release snapshot
