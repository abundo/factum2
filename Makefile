# Build directory
BUILD_DIR := build

# Install directory
INSTALL_DIR := /opt/factum2

# Go build flags (for release)
GO_BUILD_FLAGS := -ldflags="-s -w"

build: factum factum-becs factum-device-sync factum-dns factum-icinga factum-icinga-notifications factum-lime factum-librenms factum-netbox factum-oxidized factum-web factum-worker

# JS deps can ship stray .go files (e.g. flatted); they are not this module.
GO_PACKAGES := $(shell go list ./... | grep -v /node_modules/)

test:
	@output=$$(go test $(GO_PACKAGES) 2>&1); status=$$?; printf '%s\n' "$$output" | grep -v '\[no test files\]'; exit $$status

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

factum:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum cmd/factum/factum-cli.go
factum-becs:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-becs cmd/becs/factum-becs-cli.go
factum-device-sync:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-device-sync cmd/device-sync/factum-device-sync-cli.go
factum-dns:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-dns cmd/dns/factum-dns-cli.go
factum-icinga:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-icinga cmd/icinga/factum-icinga-cli.go
factum-icinga-notifications:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-icinga-notifications cmd/icinga-notifications/factum-icinga-notifications.go
factum-lime:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-lime cmd/lime/factum-lime-cli.go
factum-librenms:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-librenms cmd/librenms/factum-librenms-cli.go
factum-netbox:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-netbox cmd/netbox/factum-netbox-cli.go
factum-oxidized:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-oxidized cmd/oxidized/factum-oxidized-cli.go
factum-web:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-web cmd/web/factum-web-cli.go
factum-worker:
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/factum-worker cmd/worker/factum-worker-cli.go

# Builds the Vue SPA into web/static/vue, which factum-web serves directly.
frontend:
	cd web/frontend && npm ci && npm run build

# Self-contained release binary: static/, templates/ and the built frontend
# (web/static/vue, are embedded into the binary via go:embed , so it needs no web/ directory
# alongside it at runtime.
factum-web-release: frontend
	@mkdir -p $(BUILD_DIR)
	@go build $(GO_BUILD_FLAGS) -tags release -o $(BUILD_DIR)/factum-web cmd/web/factum-web-cli.go

release: factum factum-becs factum-device-sync factum-dns factum-icinga factum-icinga-notifications factum-lime factum-librenms factum-netbox factum-oxidized factum-web-release factum-worker

install: release
	install -m 755 $(BUILD_DIR)/* $(INSTALL_DIR)
	sudo -v
	sudo cp examples/factum2-gui.service /etc/systemd/system
	sudo cp examples/factum2-worker.service /etc/systemd/system
	#sudo systemctl daemon-reload
	#sudo systemctl restart factum2-gui.service
	#sudo systemctl restart factum2-worker.service
