.PHONY: postgres-secret check-versions validate-versions add-version

postgres-secret:
	@set -eu; \
	secret_dir=postgres-config/secrets; \
	secret_file="$$secret_dir/postgres_password"; \
	if [ -L postgres-config ] || [ -L "$$secret_dir" ] || [ ! -d "$$secret_dir" ]; then \
		echo "refusing unsafe secret directory $$secret_dir" >&2; \
		exit 1; \
	fi; \
	if [ -L "$$secret_file" ]; then \
		echo "refusing symlink secret file $$secret_file" >&2; \
		exit 1; \
	fi; \
	if [ -e "$$secret_file" ]; then \
		if [ ! -f "$$secret_file" ] || [ ! -s "$$secret_file" ]; then \
			echo "refusing to replace invalid secret file $$secret_file" >&2; \
			exit 1; \
		fi; \
		chmod 600 "$$secret_file"; \
		exit 0; \
	fi; \
	tmp_file="$$(mktemp "$${secret_file}.tmp.XXXXXX")"; \
	trap 'rm -f "$$tmp_file"' 0 1 2 15; \
	od -An -N32 -tx1 /dev/urandom | tr -d '[:space:]' > "$$tmp_file"; \
	if [ "$$(wc -c < "$$tmp_file" | tr -d '[:space:]')" -ne 64 ]; then \
		echo "failed to generate a 256-bit PostgreSQL secret" >&2; \
		exit 1; \
	fi; \
	printf '\n' >> "$$tmp_file"; \
	chmod 600 "$$tmp_file"; \
	if ! ln "$$tmp_file" "$$secret_file" 2>/dev/null && \
		{ [ ! -f "$$secret_file" ] || [ ! -s "$$secret_file" ]; }; then \
		echo "refusing to replace invalid secret file $$secret_file" >&2; \
		exit 1; \
	fi; \
	rm -f "$$tmp_file"; \
	trap - 0 1 2 15; \
	chmod 600 "$$secret_file"; \
	echo "ready $$secret_file"

check-versions:
	./scripts/check-versions.sh

validate-versions:
	./scripts/validate-versions.sh

add-version:
	@test -n "$(PACKAGE)" && test -n "$(VERSION)" || (echo "Usage: make add-version PACKAGE=name VERSION=1.2.3"; exit 1)
	./scripts/add-version.sh "$(PACKAGE)" "$(VERSION)"
