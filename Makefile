.PHONY: check-versions validate-versions add-version

check-versions:
	./scripts/check-versions.sh

validate-versions:
	./scripts/validate-versions.sh

add-version:
	@test -n "$(PACKAGE)" && test -n "$(VERSION)" || (echo "Usage: make add-version PACKAGE=name VERSION=1.2.3"; exit 1)
	./scripts/add-version.sh "$(PACKAGE)" "$(VERSION)"
