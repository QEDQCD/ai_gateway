.PHONY: test lint dev-up dev-down

test:
	./scripts/test.sh

lint:
	./scripts/lint.sh

dev-up:
	npm --prefix web run build
	docker compose -f deploy/compose/compose.yml up -d --build

dev-down:
	docker compose -f deploy/compose/compose.yml down
