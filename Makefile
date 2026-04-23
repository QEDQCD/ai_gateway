.PHONY: test lint dev-up

test:
	./scripts/test.sh

lint:
	./scripts/lint.sh

dev-up:
	npm --prefix web run build
	docker compose -f deploy/compose/compose.yml up -d --build
