# Build directory
BUILD_DIR := build

# Install directory
INSTALL_DIR := /opt/factum2

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo none)
DATE := $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
GO_BUILD_FLAGS := -ldflags="-s -w -X github.com/abundo/factum2/internal/buildinfo.Version=$(VERSION) -X github.com/abundo/factum2/internal/buildinfo.Commit=$(COMMIT) -X github.com/abundo/factum2/internal/buildinfo.Date=$(DATE)"

.PHONY: build test test-install frontend release install snapshot dev-up dev-down dev-reset

build: factum2 factum2-becs factum2-device-sync factum2-dns factum2-driver factum2-icinga factum2-icinga-notifications factum2-lime factum2-librenms factum2-netbox factum2-oxidized factum2-prometheus factum2-web factum2-worker

# JS deps can ship stray .go files (e.g. flatted); they are not this module.
GO_PACKAGES := $(shell go list ./... | grep -v /node_modules/)

test:
	@output=$$(go test $(GO_PACKAGES) 2>&1); status=$$?; printf '%s\n' "$$output" | grep -v '\[no test files\]'; exit $$status

test-install:
	python3 -m unittest install_test.py

# Device-driver tests against the containerlab lab (see LAB_TOPOLOGY's header
# for the cEOS image it needs). Build-tagged, so plain `make test` never runs
# them; they skip themselves if FACTUM_TEST_EOS_HOST isn't reachable.
LAB_DIR := internal/drivers/testdata/clab
LAB_TOPOLOGY := $(LAB_DIR)/eos.clab.yml

lab-up:
	containerlab deploy --topo $(LAB_TOPOLOGY)

lab-down:
	containerlab destroy --topo $(LAB_TOPOLOGY) --cleanup

test-integration:
	FACTUM_TEST_EOS_HOST=$${FACTUM_TEST_EOS_HOST:-clab-factum-eos-sw1} \
	go test -tags integration -count=1 -v ./internal/drivers/

# web/LDAP/mail integration tests against a real Postgres+OpenLDAP+mailpit
# stack (see testdata/itest and DEV.md's Testing section), instead of the
# sqlite-only fakes web/*_test.go otherwise uses. Build-tagged, so plain
# `make test` never runs them.
ITEST_DIR := testdata/itest

itest-up:
	docker compose -f $(ITEST_DIR)/docker-compose.yml up -d --wait

itest-down:
	docker compose -f $(ITEST_DIR)/docker-compose.yml down -v
	@# osixia/openldap chowns its bind-mounted seed dir to its internal uid
	@# (911) on every start, which otherwise leaves testdata/itest/ldap
	@# unwritable by your own user afterward.
	docker run --rm -v $(CURDIR)/$(ITEST_DIR)/ldap:/fix alpine chown -R $(shell id -u):$(shell id -g) /fix

test-integration-web:
	FACTUM_TEST_LDAP_HOST=$${FACTUM_TEST_LDAP_HOST:-localhost} \
	FACTUM_TEST_LDAP_PORT=$${FACTUM_TEST_LDAP_PORT:-3389} \
	FACTUM_TEST_SMTP_HOST=$${FACTUM_TEST_SMTP_HOST:-localhost} \
	FACTUM_TEST_SMTP_PORT=$${FACTUM_TEST_SMTP_PORT:-11025} \
	FACTUM_TEST_MAILPIT_API=$${FACTUM_TEST_MAILPIT_API:-http://localhost:18025} \
	go test -tags integration -count=1 -v ./web/

# Laptop lab: shared Postgres + MariaDB + Redis, then NetBox / LibreNMS /
# Icinga / Oxidized / BIND. Isolated from the live instance and testdata/itest.
# See dev/README.md. Uses docker compose, or podman compose if FACTUM_COMPOSE
# is set / docker is missing.
DEV_DIR := dev
# Core lab apps (no factum). Schema is applied before factum-web starts.
LAB_CORE := postgres mysql redis netbox netbox-worker librenms librenms-dispatcher icinga oxidized dns portal

dev-up:
	$(DEV_DIR)/prepare.sh
	@test -x $(BUILD_DIR)/factum2-web || $(MAKE) build
	@test -f web/static/vue/index.html || $(MAKE) frontend
	$(DEV_DIR)/compose.sh up -d --wait --wait-timeout 300 $(LAB_CORE)
	$(DEV_DIR)/seed.sh
	$(DEV_DIR)/compose.sh up -d --wait --wait-timeout 120 factum-web factum-worker

dev-down:
	$(DEV_DIR)/compose.sh down

dev-reset:
	$(DEV_DIR)/compose.sh down -v
	$(MAKE) dev-up

factum2:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2 cmd/factum2/factum2-cli.go
factum2-becs:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-becs cmd/becs/factum2-becs-cli.go
factum2-device-sync:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-device-sync cmd/device-sync/factum2-device-sync-cli.go
factum2-dns:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-dns cmd/dns/factum2-dns-cli.go
factum2-driver:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-driver cmd/driver/factum2-driver-cli.go
factum2-icinga:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-icinga cmd/icinga/factum2-icinga-cli.go
factum2-icinga-notifications:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-icinga-notifications cmd/icinga-notifications/factum2-icinga-notifications.go
factum2-lime:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-lime cmd/lime/factum2-lime-cli.go
factum2-librenms:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-librenms cmd/librenms/factum2-librenms-cli.go
factum2-netbox:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-netbox cmd/netbox/factum2-netbox-cli.go
factum2-oxidized:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-oxidized cmd/oxidized/factum2-oxidized-cli.go
factum2-prometheus:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-prometheus cmd/prometheus/factum2-prometheus-cli.go
factum2-web:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-web cmd/web/factum2-web-cli.go
factum2-worker:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum2-worker cmd/worker/factum2-worker-cli.go

# Builds the Vue SPA into web/static/vue, which factum2-web serves directly.
frontend:
	cd web/frontend && npm ci && npm run build

# Self-contained release binary: static/, templates/ and the built frontend
# (web/static/vue, are embedded into the binary via go:embed , so it needs no web/ directory
# alongside it at runtime.
factum2-web-release: frontend
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -tags release -o $(BUILD_DIR)/factum2-web cmd/web/factum2-web-cli.go

release: factum2 factum2-becs factum2-device-sync factum2-dns factum2-driver factum2-icinga factum2-icinga-notifications factum2-lime factum2-librenms factum2-netbox factum2-oxidized factum2-prometheus factum2-web-release factum2-worker

snapshot:
	goreleaser release --snapshot --clean --skip=publish

install: release
	install -m 755 $(BUILD_DIR)/* $(INSTALL_DIR)
	sudo -v
	sudo cp examples/factum2-web.service /etc/systemd/system
	sudo cp examples/factum2-worker.service /etc/systemd/system
	#sudo systemctl daemon-reload
	#sudo systemctl restart factum2-web.service
	#sudo systemctl restart factum2-worker.service
