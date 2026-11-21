# tailworks build. GOTOOLCHAIN=local: use the installed Go (1.27.4), never download one.
export GOTOOLCHAIN := local
GO        ?= go
TS_CREDS  ?= $(HOME)/.config/tailnet-agents-design
TAILNET1  ?= tail7906d5.ts.net
WORKERD   := node_modules/.bin/workerd

# Each target declares its own .PHONY right above it (one shared .PHONY line
# conflicted on every parallel branch that added a target; see AGENTS.md).

.PHONY: all
all: build

## build: every binary into bin/. On macOS, tw-vz (the microVM tier's per-VM VMM, cgo on
## Virtualization.framework) is ad-hoc signed with the virtualization entitlement it needs.
VZ_ENTITLEMENTS := cmd/tw-vz/tw-vz.entitlements
# Dev builds are stamped <VERSION>-dev (internal/version); releases set the
# exact version (scripts/release.sh).
VERSION_LDFLAGS := -X github.com/rosskukulinski/tailworks/internal/version.Version=$(shell cat VERSION)-dev -X github.com/rosskukulinski/tailworks/internal/version.Commit=$(shell git rev-parse HEAD 2>/dev/null)
.PHONY: build
build:
	$(GO) build -ldflags "$(VERSION_LDFLAGS)" -o bin/ ./cmd/...
	@if [ "$$(uname -s)" = Darwin ]; then codesign --force --sign - --entitlements $(VZ_ENTITLEMENTS) bin/tw-vz; fi

$(WORKERD): package.json package-lock.json
	npm ci --no-audit --no-fund
	@touch $@

.PHONY: deps
deps: $(WORKERD) js-build
	$(GO) mod download

## js-build: compile the TypeScript packages (dist/ is gitignored, so clean clones need this).
.PHONY: js-build
js-build: $(WORKERD)
	npm run -w @tailworks/tailnet build
	npm run -w @tailworks/mcp build
	npm run --workspaces --if-present build

## templates-typecheck: every template's `tw init && npm install && npx tsc --noEmit`, with
## @tailworks/* from npm-pack tarballs of this checkout (issue #41; needs the network for the rest).
.PHONY: templates-typecheck
templates-typecheck: js-build
	sh scripts/templates-typecheck.sh

## test: hermetic unit + integration tests (testcontrol + local DERP/STUN + stock workerd).
.PHONY: test
test: deps test-go test-js

.PHONY: test-go
# GO_TEST_SKIP: a go test -skip regexp (CI quarantines known failures with it; see ci.yml).
# Read from the environment as-is, so a regexp's "$" survives make.
# -timeout 20m: cmd/tw alone takes ~9-10 min under -race on CI; the default 10m is too tight.
GO_TEST_SKIP ?=
export GO_TEST_SKIP
test-go:
	$(GO) test -race -count=1 -timeout 20m $(if $(GO_TEST_SKIP),-skip "$$GO_TEST_SKIP") ./netd/... ./internal/... ./cmd/... ./mgmt/... ./hostd/... ./security/... ./guest/... ./agent/... ./docs/... ./schema/...

.PHONY: test-js
test-js:
	npm test --silent

## test-sdk: M5 SDK conformance (hermetic): real Cloudflare Agents SDK and Vercel AI SDK
## apps on tw-hostd + stock workerd over testcontrol + local DERP; mock LLM, no keys.
.PHONY: test-sdk
test-sdk: deps
	$(GO) test -tags sdk -count=1 -v -timeout 10m ./e2e/sdk/...

## test-images: ADR-032 D3 on the developer machine: tw deploy's image builder for real (docker
## buildx, both arches, alias contexts from tailworks' registry) and the registry conformance run with
## crane (CRANE=<path>, default: crane on PATH; go install github.com/google/go-containerregistry/cmd/crane@v0.20.6).
CRANE ?= $(shell command -v crane)
.PHONY: test-images
test-images:
	TW_TEST_DOCKER=1 TW_CRANE="$(CRANE)" $(GO) test -count=1 -v -run 'TestBuildAndPush|TestCraneConformance|TestCLIImages' ./internal/imagebuild/ ./internal/registry/ ./cmd/tw/

## e2e-placement: smart placement (ADR-033) and D1 across a restart and a move (ADR-038), hermetic: real tw-mgmt + two tw-hostd processes on
## testcontrol + local DERP, a tag:db peer, one host forced onto DERP; placement, stickiness, --replace.
.PHONY: e2e-placement
e2e-placement: deps
	$(GO) test -tags e2eplacement -count=1 -v -timeout 10m ./e2e/placement/...

## e2e-locks: locks and leader election on DO leases (docs/locks.md), hermetic: real tw-mgmt + two
## tw-hostd processes, default timings; one host cut off from mgmt while still on the tailnet.
.PHONY: e2e-locks
e2e-locks: deps
	$(GO) test -tags e2elocks -count=1 -v -timeout 10m ./e2e/locks/...

## e2e-queues: Queues (ADR-039), hermetic: real tw-mgmt + two tw-hostd processes on testcontrol +
## local DERP; a producer on one host, a consumer on the other; nothing lost across a hostd restart,
## a crash of hostd and workerd mid-batch, and a lease move (deploy --replace).
.PHONY: e2e-queues
e2e-queues: deps
	$(GO) test -tags e2equeues -count=1 -v -timeout 10m ./e2e/queues/...
## e2e-observability: logs, metrics and traces (ADR-045), hermetic: real tw-mgmt + tw-hostd processes
## on testcontrol + local DERP, a caller, an echo and an OTLP collector node; traceparent, tw logs, /metrics, OTLP.
.PHONY: e2e-observability
e2e-observability: deps
	$(GO) test -tags e2eobs -count=1 -v -timeout 10m ./e2e/observability/...

## test-pg: the mgmt Store tests against Postgres 17 in OrbStack docker (throwaway container), or,
## with TW_TEST_POSTGRES_DSN set, against that database (CI's service container; nightly.yml).
PG_CTX  ?= orbstack
PG_PORT ?= 55433
.PHONY: test-pg
test-pg:
	sh scripts/test-pg.sh "$(PG_CTX)" "$(PG_PORT)"

## e2e-upgrade: the upgrade release gate (ADR-041 §9), hermetic: the previous
## release's tw-mgmt + tw-hostd (TW_UPGRADE_FROM: a git ref, default the newest published
## GitHub release (scripts/upgrade-from.sh), built in a temporary worktree; "self", the default
## while none is published, stamps this tree TW_UPGRADE_FROM_VERSION) serve a
## Durable Object workload; mgmt then the host move to this tree on the same state; storage, node
## identity and the store must survive, the old mgmt must still open the store, a 9.0.0 host is refused.
TW_UPGRADE_FROM ?= $(shell sh scripts/upgrade-from.sh)
.PHONY: e2e-upgrade
e2e-upgrade: deps
	TW_UPGRADE_FROM="$(TW_UPGRADE_FROM)" $(GO) test -tags e2eupgrade -count=1 -v -timeout 20m ./e2e/upgrade/...

## e2e-local: the tw-netd binary as a real process against a local control plane + workerd.
.PHONY: e2e-local
e2e-local: deps
	$(GO) test -tags e2e -count=1 -v ./e2e/local/...

## e2e-wrangler: a wrangler.toml-only project through `tw dev` (tailworks + hostd + workerd), bundled by the
## built-in esbuild, from a wrangler --outdir, and by the wrangler CLI (that one skips without wrangler).
.PHONY: e2e-wrangler
e2e-wrangler: deps
	$(GO) test -tags e2e -count=1 -v ./e2e/wrangler/...

## e2e-real: the identity/egress/kill suite on tailnet 1 (runs only if the creds exist).
.PHONY: e2e-real
e2e-real: deps
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-real: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2ereal -count=1 -v -timeout 15m ./e2e/real/...

## e2e-headscale: the Headscale profile (ADR-015) against a pinned Headscale release in Docker
## (policy conformance, node lifecycle, `tw up --control=headscale` end to end, the v0.29 gate).
## HEADSCALE_IMAGE picks another release; TW_HEADSCALE_URL (+ _API_KEY_FILE, _BASE_DOMAIN) an
## existing server. Skips without docker.
HEADSCALE_IMAGE ?= headscale/headscale:v0.29.7
.PHONY: e2e-headscale
e2e-headscale: deps
	HEADSCALE_IMAGE=$(HEADSCALE_IMAGE) $(GO) test -tags e2eheadscale -count=1 -v -timeout 20m ./e2e/headscale/...

## e2e-coldid: cold-identity breakdown (create → own node reachable, per stage), hermetic.
## TW_COLDID_N objects; TW_COLDID_DIAL=ready makes the caller wait for "ready". Results: e2e/coldid/results/.
.PHONY: e2e-coldid
e2e-coldid:
	$(GO) test -tags e2e -count=1 -v -timeout 15m -run TestColdIdentityLocal ./e2e/coldid/

## e2e-coldid-real: the same on tailnet 1 (production control; ttest-fi-* nodes, deleted by ID afterwards).
.PHONY: e2e-coldid-real
e2e-coldid-real:
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-coldid-real: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2ereal -count=1 -v -timeout 15m -run TestColdIdentityReal ./e2e/coldid/

## guest-image / guest-kernel / vm-assets: the microVM guest (Linux only; reproducible; inputs
## pinned in guest/inputs.sh) for GUEST_ARCH (Go's name, amd64 or arm64; default: this machine).
## Either arch builds on either (cross: the amd64 kernel needs gcc-x86-64-linux-gnu on arm64,
## the arm64 one gcc-aarch64-linux-gnu on amd64).
## guest-image: the rootfs: workerd + tw-guestd + glibc (ubuntu-base 24.04) + a CA bundle.
GUEST_ARCH ?= $(shell $(GO) env GOARCH)
GUEST_OUT  ?= guest/out/$(GUEST_ARCH)
KERNEL_OUT ?= guest/out/kernel-$(GUEST_ARCH)
.PHONY: guest-image
guest-image: $(WORKERD)
	TARGETARCH=$(GUEST_ARCH) guest/build-image.sh $(GUEST_OUT)
## guest-kernel: linux 6.1 LTS with Firecracker's CI guest config + guest/kernel/tw.config
## (erofs, overlayfs). Reused when its inputs are unchanged (KERNEL_OUT/kernel.json).
.PHONY: guest-kernel
guest-kernel:
	TARGETARCH=$(GUEST_ARCH) guest/build-kernel.sh $(KERNEL_OUT)

## e2e-vm: the microVM tier in Firecracker (Linux + KVM, native GUEST_ARCH; run inside Lima
## tw-dev or on an amd64 KVM host; needs sudo). TW_VM_KERNEL defaults to the guest-kernel
## build (set it to use another kernel, e.g. ~/fc/vmlinux, without building one).
DEFAULT_VM_KERNEL := $(abspath $(KERNEL_OUT))/vmlinux
TW_VM_KERNEL ?= $(DEFAULT_VM_KERNEL)
VM_KERNEL_DEP = $(if $(filter $(DEFAULT_VM_KERNEL),$(TW_VM_KERNEL)),guest-kernel)
# 2 MiB pages for guest RAM (runtime sysctl inside the Lima VM, not persisted):
# without them, workerd start-up under nested virt is ~8x slower.
VM_HUGEPAGES ?= 2048
# TW_VM_VCPUS / TW_VM_BOOTARGS (optional) pass through to the tests (sudo drops the env), as do
# fast-vm's TW_VM_BENCH_DEPLOYS / TW_VM_E2E_SNAPSHOT / TW_VM_E2E_SNAPSHOT_REFILL.
VM_ENV = PATH="$$PATH" TW_VM_KERNEL="$(TW_VM_KERNEL)" TW_VM_ROOTFS="$(CURDIR)/$(GUEST_OUT)/rootfs.ext4" \
	TW_VM_BENCH_DEPLOYS="$(TW_VM_BENCH_DEPLOYS)" TW_VM_E2E_SNAPSHOT="$(TW_VM_E2E_SNAPSHOT)" TW_VM_E2E_SNAPSHOT_REFILL="$(TW_VM_E2E_SNAPSHOT_REFILL)" \
	$(if $(TW_VM_VCPUS),TW_VM_VCPUS="$(TW_VM_VCPUS)") $(if $(TW_VM_BOOTARGS),TW_VM_BOOTARGS="$(TW_VM_BOOTARGS)")
.PHONY: e2e-vm
e2e-vm: deps guest-image $(VM_KERNEL_DEP)
	@[ "$$(uname -s)" = Linux ] || { echo "e2e-vm: Linux only; run it inside Lima tw-dev"; exit 1; }
	@[ "$(GUEST_ARCH)" = "$$($(GO) env GOARCH)" ] || { echo "e2e-vm: a $(GUEST_ARCH) guest can't boot on this $$($(GO) env GOARCH) host"; exit 1; }
	@[ "$$(cat /proc/sys/vm/nr_hugepages)" -ge $(VM_HUGEPAGES) ] || sudo sysctl -qw vm.nr_hugepages=$(VM_HUGEPAGES)
	$(GO) test -c -tags e2evm -o bin/e2evm.test ./e2e/vm
	$(GO) test -c -o bin/vmsmoke.test ./hostd/vm
	sudo env $(VM_ENV) bin/vmsmoke.test -test.v -test.count=1
	cd e2e/vm && sudo env $(VM_ENV) ../../bin/e2evm.test -test.v -test.count=1 -test.timeout 15m
## e2e-container: ADR-032 acceptance on Firecracker (Linux + KVM, in Lima tw-dev; needs sudo):
## the Sandbox SDK 0.x examples/minimal and the 1.0 flow run unmodified through hostd + workerd
## + one jailed VM per container, plus lifecycle/isolation checks and measurements.
## CTR_IMAGES is the dir guest/images/build-sandbox-images.sh wrote (run that on the Mac first);
## The guest kernel (TW_VM_KERNEL, the guest-kernel build) has erofs, so containers boot it too.
CTR_IMAGES ?= $(CURDIR)/guest/out/images
CTR_KERNEL ?= $(TW_VM_KERNEL)
CTR_APPS   ?= /tmp/tw-e2e-container-apps
CTR_ENV = $(VM_ENV) TW_VM_CONTAINER_KERNEL="$(CTR_KERNEL)" TW_CTR_IMAGES="$(CTR_IMAGES)" TW_CTR_APPS="$(CTR_APPS)"
.PHONY: e2e-container
e2e-container: deps guest-image $(VM_KERNEL_DEP)
	@[ "$$(uname -s)" = Linux ] || { echo "e2e-container: Linux only; run it inside Lima tw-dev"; exit 1; }
	@[ -f "$(CTR_IMAGES)/images.json" ] || { echo "e2e-container: no $(CTR_IMAGES)/images.json (run guest/images/build-sandbox-images.sh on the Mac)"; exit 1; }
	@[ "$$(cat /proc/sys/vm/nr_hugepages)" -ge $(VM_HUGEPAGES) ] || sudo sysctl -qw vm.nr_hugepages=$(VM_HUGEPAGES)
	@[ -f "$(CTR_APPS)/unmodified.txt" ] || scripts/e2e-container-prep.sh $(CTR_APPS)
	$(GO) test -c -o bin/vmsmoke.test ./hostd/vm
	$(GO) test -c -tags e2econtainer -o bin/e2econtainer.test ./e2e/container
	sudo env $(CTR_ENV) bin/vmsmoke.test -test.v -test.count=1 -test.run TestContainerSmoke
	cd e2e/container && sudo env $(CTR_ENV) ../../bin/e2econtainer.test -test.v -test.count=1 -test.timeout 20m

## e2e-container-build (developer machine, docker buildx) then e2e-container-registry (Lima):
## ADR-032 D3 end to end. tw deploy's builder builds the acceptance examples' own Dockerfiles
## (alias map as BuildKit named contexts) into CTR_BUILT; in Lima they are pushed through tailworks'
## registry, pinned by digest at deploy, pulled by hostd (no other image source) and booted.
## CTR_APPS must hold the prepared examples on the build machine (scripts/e2e-container-prep.sh).
CTR_BUILT ?= $(CURDIR)/.scratch/ctr-built
.PHONY: e2e-container-build
e2e-container-build:
	TW_CTR_APPS="$(CTR_APPS)" TW_CTR_BUILT="$(CTR_BUILT)" $(GO) test -count=1 -tags e2eimages -v -run TestBuildExampleImages ./e2e/container/
.PHONY: e2e-container-registry
e2e-container-registry: deps guest-image $(VM_KERNEL_DEP)
	@[ "$$(uname -s)" = Linux ] || { echo "e2e-container-registry: Linux only; run it inside Lima tw-dev"; exit 1; }
	@[ -f "$(CTR_BUILT)/built.json" ] || { echo "e2e-container-registry: no $(CTR_BUILT)/built.json (make e2e-container-build on the Mac)"; exit 1; }
	@[ "$$(cat /proc/sys/vm/nr_hugepages)" -ge $(VM_HUGEPAGES) ] || sudo sysctl -qw vm.nr_hugepages=$(VM_HUGEPAGES)
	@[ -f "$(CTR_APPS)/unmodified.txt" ] || scripts/e2e-container-prep.sh $(CTR_APPS)
	$(GO) test -c -tags e2econtainer -o bin/e2econtainer.test ./e2e/container
	cd e2e/container && sudo env $(CTR_ENV) TW_CTR_BUILT="$(CTR_BUILT)" ../../bin/e2econtainer.test -test.v -test.count=1 -test.timeout 20m -test.run TestE2EContainerRegistry

## vm-assets: the microVM guest for the linux-$(GUEST_ARCH) release (Linux only; Lima tw-dev
## builds both arches): the guest image, the kernel (TW_VM_KERNEL, + its kernel.json) and
## Firecracker + jailer (the pinned release, checksum-verified; FC_BIN_DIR=<dir> copies local
## binaries instead) in VM_ASSETS.
FC_BIN_DIR ?=
VM_ASSETS  ?= dist/vm-assets-$(GUEST_ARCH)
.PHONY: vm-assets
vm-assets: guest-image $(VM_KERNEL_DEP)
	@[ "$$(uname -s)" = Linux ] || { echo "vm-assets: Linux only; run it inside Lima tw-dev"; exit 1; }
	mkdir -p $(VM_ASSETS)
	cp -f $(GUEST_OUT)/rootfs.ext4 $(GUEST_OUT)/manifest.json $(VM_ASSETS)/
	cp -f $(TW_VM_KERNEL) $(VM_ASSETS)/vmlinux
	@k="$$(dirname "$(TW_VM_KERNEL)")/kernel.json"; if [ -f "$$k" ]; then cp -f "$$k" $(VM_ASSETS)/kernel.json; else rm -f $(VM_ASSETS)/kernel.json; fi
	@if [ -n "$(FC_BIN_DIR)" ]; then cp -f $(FC_BIN_DIR)/firecracker $(FC_BIN_DIR)/jailer $(VM_ASSETS)/; \
	else TARGETARCH=$(GUEST_ARCH) guest/fetch-firecracker.sh $(VM_ASSETS); fi
	node scripts/check-arch.mjs linux $(GUEST_ARCH) $(VM_ASSETS)/firecracker $(VM_ASSETS)/jailer
	@if [ "$(GUEST_ARCH)" = "$$($(GO) env GOARCH)" ]; then $(VM_ASSETS)/firecracker --version | head -1; fi

## vz-assets: the macOS microVM guest (vz backend): the arm64 guest kernel with virtiofs
## (guest/build-kernel-vz.sh) as vmlinux + the guest image, built in Lima tw-dev (or on arm64
## Linux) and cached in VZ_ASSETS by input hash (scripts/vz-assets-lima.sh).
VZ_ASSETS ?= dist/vz-assets-arm64
.PHONY: vz-assets
vz-assets:
	sh scripts/vz-assets-lima.sh $(VZ_ASSETS)

## vz-kernel: just the vz guest kernel (arm64 Linux only, e.g. inside Lima tw-dev) into bin/vz.
.PHONY: vz-kernel
vz-kernel:
	guest/build-kernel-vz.sh bin/vz

## e2e-vz: the microVM tier on this Mac (Virtualization.framework; no sudo): the vz smoke,
## volume and Rosetta tests (hostd/vm), then E2E-VM (e2e/vm) with the vz backend: tier
## selection, attributed egress per object, DO storage on the encrypted volume, isolation,
## no keys in the guest, no host network from the guest, kill → restart, redeploy.
VZ_ENV = TW_VZ_VMM="$(CURDIR)/bin/tw-vz" TW_VM_KERNEL="$(CURDIR)/$(VZ_ASSETS)/vmlinux" TW_VM_ROOTFS="$(CURDIR)/$(VZ_ASSETS)/rootfs.ext4"
.PHONY: e2e-vz
e2e-vz: deps build vz-assets
	@[ "$$(uname -s)" = Darwin ] || { echo "e2e-vz: macOS only"; exit 1; }
	$(VZ_ENV) TW_VM_ROSETTA=$$(bin/tw-vz -check $(VZ_ASSETS)/vmlinux | grep -q rosetta=installed && echo 1) \
		$(GO) test -count=1 -v -run 'TestVZ|TestDgram' ./hostd/vm
	cd e2e/vm && $(VZ_ENV) $(GO) test -tags e2evm -count=1 -v -timeout 15m .

## e2e-vz-container: ADR-032 acceptance on this Mac (vz; no sudo; needs mkfs.erofs:
## brew install erofs-utils): TestContainerSmoke, then E2E-CONTAINER. VZ_ROSETTA=1 (the
## default when Rosetta for Linux is installed) runs the STOCK linux/amd64 images under Rosetta,
## built with PLATFORM=linux/amd64 guest/images/build-sandbox-images.sh into CTR_IMAGES_AMD64;
## VZ_ROSETTA=0 runs the arm64 set (CTR_IMAGES) through the alias map.
CTR_IMAGES_AMD64 ?= $(CURDIR)/guest/out/images-amd64
VZ_ROSETTA ?= $(shell bin/tw-vz -check $(VZ_ASSETS)/vmlinux 2>/dev/null | grep -q rosetta=installed && echo 1 || echo 0)
VZ_CTR_IMAGES = $(if $(filter 1,$(VZ_ROSETTA)),$(CTR_IMAGES_AMD64),$(CTR_IMAGES))
VZ_CTR_ENV = $(VZ_ENV) TW_VM_ROSETTA=$(filter 1,$(VZ_ROSETTA)) TW_CTR_IMAGES="$(VZ_CTR_IMAGES)" TW_CTR_APPS="$(CTR_APPS)"
.PHONY: e2e-vz-container
e2e-vz-container: deps build vz-assets
	@[ "$$(uname -s)" = Darwin ] || { echo "e2e-vz-container: macOS only"; exit 1; }
	@[ -f "$(VZ_CTR_IMAGES)/images.json" ] || { echo "e2e-vz-container: no $(VZ_CTR_IMAGES)/images.json (guest/images/build-sandbox-images.sh; PLATFORM=linux/amd64 for Rosetta)"; exit 1; }
	@[ -f "$(CTR_APPS)/unmodified.txt" ] || scripts/e2e-container-prep.sh $(CTR_APPS)
	$(VZ_CTR_ENV) $(GO) test -count=1 -v -run 'TestContainerSmoke|TestVZContainer' ./hostd/vm
	cd e2e/container && $(VZ_CTR_ENV) $(GO) test -tags e2econtainer -count=1 -v -timeout 20m .

## e2e-linux-vm: L3 E2E from the installed release on a FRESH nested-virt Lima VM
## (twl-kvm-u24; created if missing, stopped at the end): the linux-arm64 release with the
## microVM assets is built in Lima tw-dev, copied in with limactl copy, then install.sh ->
## sudo tw host setup --microvm -> tw up -> agent + mcp with tier microvm -> checks (a)-(f) from
## this Mac over tailnet 1, storage persistence across a VM kill and a redeploy, no keys in
## the guest, tw host setup --undo.
LINUX_VM ?= twl-kvm-u24
.PHONY: e2e-linux-vm
e2e-linux-vm:
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-linux-vm: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TS_CREDS="$(TS_CREDS)" TAILNET1="$(TAILNET1)" sh scripts/e2e-linux-vm.sh $(LINUX_VM)

## e2e-linux-reboot: L3 DO storage across host reboots of the same fresh VM (clean, then a
## power cut): helper back via systemd, tw up, same object node, data present, volume re-opens.
.PHONY: e2e-linux-reboot
e2e-linux-reboot:
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-linux-reboot: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TW_E2E_RUN=TestE2ELinuxReboot TS_CREDS="$(TS_CREDS)" TAILNET1="$(TAILNET1)" sh scripts/e2e-linux-vm.sh $(LINUX_VM)

## e2e-svc: Tailscale Services on tailnet 1 (docs/services.md): the tssvc mechanism, then a workload with
## tailnet.service via the tailworks CLI (deploy → VIP with caller identity → redeploy → kill → down).
.PHONY: e2e-svc
e2e-svc: deps
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-svc: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2ereal -count=1 -v -run TestRealService ./internal/tssvc/ && \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2esvc -count=1 -v -timeout 20m ./e2e/svc/...

## e2e-mm-svc: H12 on tailnet 1: two tw-mgmt replicas hosting svc:ttest-mgmt. Needs TW_MM_PG_DSN: a
## Postgres of its own (never the multimgmt suite's tw-mm-pg container, which other runs may be using).
.PHONY: e2e-mm-svc
e2e-mm-svc:
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ] || [ -z "$$TW_MM_PG_DSN" ]; then \
		echo "e2e-mm-svc: SKIPPED (needs $(TS_CREDS)/tailnet1_api.env and TW_MM_PG_DSN)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags 'e2emultimgmt e2ereal' -run TestRealMgmtService -count=1 -v -timeout 20m ./e2e/multimgmt/

## e2e-mac: E2E-MAC via the tailworks CLI on tailnet 1 (tw up → deploy agent+mcp → checks a–f → tw down → zero leftovers).
.PHONY: e2e-mac
e2e-mac: deps
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-mac: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2emac -count=1 -v -timeout 20m ./e2e/mac/...

## e2e-multi: E2E-MULTI on this Mac: one host serves tenant A (tailnet 1, `tw up`) and tenant B
## (tailnet-2, `tw tenant add`, name discovered) with the same agent app; isolation checks a–f;
## zero leftovers in both tailnets. Runs only if both credential files exist.
.PHONY: e2e-multi
e2e-multi: deps
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ] || [ ! -f "$(TS_CREDS)/tailnet2_api.env" ]; then \
		echo "e2e-multi: SKIPPED (needs $(TS_CREDS)/tailnet1_api.env and tailnet2_api.env)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_API_KEY_FILE_B="$(TS_CREDS)/tailnet2_api.env" \
		TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2emulti -count=1 -v -timeout 25m ./e2e/multi/...

## e2e-oauth: M12 on tailnet 1: `tw up --oauth-client-file` (mint mode oauth: an OAuth client with
## auth_keys + devices:core + policy_file:read, tag:tw; no browser, no Funnel, no API key given to tailworks)
## → checks (a)–(f), `tw link` over https and http, remote deploy/kill/audit, zero leftovers. The API
## key creates and deletes the OAuth client, mints the test's own nodes and cleans up by ID.
.PHONY: e2e-oauth
e2e-oauth: deps
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-oauth: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2eoauth -count=1 -v -timeout 25m ./e2e/oauth/...

## e2e-public: ADR-029 on tailnet 1: public ingress over real Funnel. `tw up --oauth-client-file`,
## a workload with tailworks.jsonc "public" routes, fetched through public DNS like an internet client:
## public requester (never a tailnet caller), spoofed headers dropped, route allowlist, no public
## object creation (opt-in up to maxObjects), `tw expose off|on`, audit, doctor probe, zero leftovers.
## The only policy write it may make is the tag:tw-public tagOwner + its funnel nodeAttr.
.PHONY: e2e-public
e2e-public: deps
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-public: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		$(GO) test -tags e2epublic -count=1 -v -timeout 30m ./e2e/public/...

## release: self-contained tarballs in dist/ (darwin-arm64, linux-arm64, linux-amd64): Go binaries
## (CGO_ENABLED=0, esbuild built in), pinned workerd, templates + vendored node_modules,
## install.sh, VERSION, SHA256SUMS. darwin binaries are ad-hoc signed (run on macOS).
# Expanded once (:=): a recursive variable would be re-evaluated per use, so an
# edit during `make release` would name the e2e tarball differently.
ifndef RELEASE_VERSION
RELEASE_VERSION := $(shell cat VERSION)-$(shell git rev-parse --short=8 HEAD)$(shell git diff --quiet HEAD -- . ':!STATUS.md' ':!status' || echo -dirty)
endif
RELEASE_TARGETS ?= darwin-arm64 linux-arm64 linux-amd64
## The linux-arm64 and linux-amd64 microVM guests (share/tailworks/vm: kernel, rootfs, firecracker,
## jailer) are built by scripts/vm-assets-lima.sh: on Linux directly, on macOS inside Lima
## tw-dev (both arches there; cached in dist/vm-assets-<arch> by input hash).
## RELEASE_VM_ASSETS=auto (default): include them when a builder is available, else warn and
## ship without the microVM tier (release.json "microvm": false); 1: required; 0: never.
## TW_VM_ASSETS_arm64 / TW_VM_ASSETS_amd64 override.
## The darwin-arm64 microVM guest (share/tailworks/vm: the vz kernel, the same rootfs) comes from
## scripts/vz-assets-lima.sh the same way (TW_VZ_ASSETS overrides); its bin/tw-vz is
## signed with the virtualization entitlement.
RELEASE_VM_ASSETS ?= auto
.PHONY: release
release: deps
	rm -rf dist/stage dist/tailworks-*.tar.gz dist/SHA256SUMS
	@for a in arm64 amd64; do \
		eval "va=\$${TW_VM_ASSETS_$$a:-}"; \
		if [ -z "$$va" ] && [ "$(RELEASE_VM_ASSETS)" != 0 ] && echo "$(RELEASE_TARGETS)" | grep -q "linux-$$a"; then \
			if sh scripts/vm-assets-lima.sh dist/vm-assets-$$a $$a; then va="$(CURDIR)/dist/vm-assets-$$a"; \
			elif [ "$(RELEASE_VM_ASSETS)" = 1 ]; then echo "release: linux-$$a microVM assets required (RELEASE_VM_ASSETS=1)"; exit 1; \
			else echo "release: WARNING: the linux-$$a tarball will have NO microVM guest (see scripts/vm-assets-lima.sh)"; fi; \
		fi; \
		eval "TW_VM_ASSETS_$$a=\$$va"; export TW_VM_ASSETS_$$a; \
	done; \
	vz="$${TW_VZ_ASSETS:-}"; \
	if [ -z "$$vz" ] && [ "$(RELEASE_VM_ASSETS)" != 0 ] && echo "$(RELEASE_TARGETS)" | grep -q darwin-arm64; then \
		if sh scripts/vz-assets-lima.sh $(VZ_ASSETS); then vz="$(CURDIR)/$(VZ_ASSETS)"; \
		elif [ "$(RELEASE_VM_ASSETS)" = 1 ]; then echo "release: microVM assets required (RELEASE_VM_ASSETS=1)"; exit 1; \
		else echo "release: WARNING: the darwin-arm64 tarball will have NO microVM guest (see scripts/vz-assets-lima.sh)"; fi; \
	fi; \
	TW_VZ_ASSETS="$$vz" sh scripts/release.sh $(RELEASE_VERSION) $(RELEASE_TARGETS)

## e2e-mbp: E2E-MAC on the clean MacBook `mbp` from the release tarball (skips without
## the tailnet-1 key file or `ssh mbp`). Tailworks runs on mbp; the test client + echo run here.
MBP ?= mbp
.PHONY: e2e-mbp
e2e-mbp: release
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-mbp: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	if ! ssh -o BatchMode=yes -o ConnectTimeout=10 $(MBP) true 2>/dev/null; then \
		echo "e2e-mbp: SKIPPED (ssh $(MBP) not reachable)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" TW_E2E_SSH_HOST="$(MBP)" \
		TW_E2E_TARBALL="$(CURDIR)/dist/tailworks-$(RELEASE_VERSION)-darwin-arm64.tar.gz" \
		sh scripts/e2e-mbp.sh $(MBP) -tags e2embp -count=1 -v -timeout 25m ./e2e/mbp/...

## e2e-linux: E2E-LINUX: the e2e-mbp checks (a)–(f) + tw doctor + `tw up --service`
## (systemd user unit; survives `systemctl --user restart`) on FRESH Lima VMs
## twl-<distro> that mount nothing from this Mac, from the linux release tarballs.
## tailworks runs in the VM; the test client + echo run here. ≤2 VMs at a time; each is
## stopped (kept) when its row is done. LINUX_DISTROS picks rows (see
## scripts/e2e-linux-one.sh; debian11-arm64 / alpine-arm64 check doctor's refusal).
LINUX_DISTROS ?= ubuntu24-amd64 ubuntu24-arm64 debian12-arm64 ubuntu22-arm64
.PHONY: e2e-linux
e2e-linux: release
	@if [ ! -f "$(TS_CREDS)/tailnet1_api.env" ]; then \
		echo "e2e-linux: SKIPPED ($(TS_CREDS)/tailnet1_api.env not found)"; exit 0; fi; \
	if ! command -v limactl >/dev/null; then echo "e2e-linux: SKIPPED (no limactl)"; exit 0; fi; \
	TW_E2E_API_KEY_FILE="$(TS_CREDS)/tailnet1_api.env" TW_E2E_TAILNET="$(TAILNET1)" \
		TW_E2E_TARBALL_arm64="$(CURDIR)/dist/tailworks-$(RELEASE_VERSION)-linux-arm64.tar.gz" \
		TW_E2E_TARBALL_amd64="$(CURDIR)/dist/tailworks-$(RELEASE_VERSION)-linux-amd64.tar.gz" \
		sh scripts/e2e-linux.sh $(LINUX_DISTROS)

## cli-docs: regenerate docs/cli.md (every command's --help output) from cmd/tw;
## TestCLIReferenceUpToDate fails when it is stale.
.PHONY: cli-docs
cli-docs:
	TW_UPDATE_CLI_DOCS=1 $(GO) test ./cmd/tw -run '^TestCLIReferenceUpToDate$$' -count=1

## api: regenerate everything derived from api/openapi.yaml (the /v1 source of truth):
## Go server interfaces + types + embedded spec (mgmt/api/v1), the Go client
## (hostd/mgmtapi/v1) and docs/contracts/mgmt-api.md. oapi-codegen is pinned as a
## go.mod tool; nothing here needs cgo or a network once modules are cached.
.PHONY: api
api:
	$(GO) tool oapi-codegen -config api/oapi-codegen.server.yaml api/openapi.yaml
	$(GO) tool oapi-codegen -config api/oapi-codegen.client.yaml api/openapi.yaml
	$(GO) run ./api/render -in api/openapi.yaml -out docs/contracts/mgmt-api.md

## api-lint: Redocly lint of the spec (pinned version, via npx; warnings fail: see redocly.yaml).
REDOCLY ?= npx -y @redocly/cli@2.76.0
.PHONY: api-lint
api-lint:
	$(REDOCLY) lint api/openapi.yaml

## api-check: CI gate: the generated code and the rendered contract are current.
.PHONY: api-check
api-check: api
	@if ! git diff --quiet --exit-code -- mgmt/api/v1 hostd/mgmtapi/v1 docs/contracts/mgmt-api.md; then \
		echo "api-check: generated files are stale; run 'make api' and commit:"; \
		git --no-pager diff --stat -- mgmt/api/v1 hostd/mgmtapi/v1 docs/contracts/mgmt-api.md; exit 1; fi
	$(GO) test -count=1 ./mgmt/api/...

## compat-check: the README's "Compatibility at a glance" rows match docs/COMPAT.md, and every
## ✅/⚠️ row there names tests that exist (internal/compatdoc).
.PHONY: compat-check
compat-check:
	$(GO) run ./internal/compatdoc/compatcheck

## changes-check: release-note fragments (changes/*.md) are valid, and a branch that touches an
## upgrade-relevant path (migrations, the API spec, the workerd pin, …) names it in one (changes/README.md).
.PHONY: changes-check
changes-check:
	sh scripts/check-changes.sh

## install-test: install.sh upgrade keeps .prev, --rollback swaps it back (hermetic, fake releases).
.PHONY: install-test
install-test:
	sh scripts/install_test.sh

.PHONY: lint
lint: api-check compat-check changes-check
	@out=$$(gofmt -l $$(git ls-files -co --exclude-standard '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	@$(MAKE) --no-print-directory staticcheck
	@$(MAKE) --no-print-directory vulncheck
	$(GO) vet -tags e2e ./e2e/local/...
	$(GO) vet -tags e2e ./e2e/wrangler/...
	$(GO) vet -tags e2eupgrade ./e2e/upgrade/...
	$(GO) vet -tags e2ereal ./e2e/real/...
	GOOS=linux $(GO) vet ./hostd/vm/... ./guest/... ./cmd/tw-hostd/... ./cmd/tw-vmhelper/... ./cmd/tw/...
	GOOS=linux $(GO) vet -tags e2evm ./e2e/vm/...
	GOOS=linux $(GO) vet -tags e2econtainer ./e2e/container/...
	$(GO) vet -tags e2eimages ./e2e/container/...
	GOOS=darwin $(GO) vet -tags e2evm ./e2e/vm/...
	GOOS=darwin $(GO) vet -tags e2econtainer ./e2e/container/...
	$(GO) vet -tags e2elinuxvm ./e2e/linuxvm/...
	$(GO) vet -tags e2emac ./e2e/mac/...
	$(GO) vet -tags e2esvc ./e2e/svc/...
	$(GO) vet -tags 'e2emultimgmt e2ereal' ./e2e/multimgmt/...
	$(GO) vet -tags e2ereal ./internal/tssvc/...
	$(GO) vet -tags e2emulti ./e2e/multi/...
	$(GO) vet -tags e2eoauth ./e2e/oauth/...
	$(GO) vet -tags e2eheadscale ./e2e/headscale/...
	$(GO) vet -tags e2epublic ./e2e/public/...
	$(GO) vet -tags e2embp ./e2e/mbp/...
	$(GO) vet -tags e2elinux ./e2e/linux/...
	$(GO) vet -tags sdk ./e2e/sdk/...
	$(GO) vet -tags nodetier ./e2e/node/...
	$(GO) vet -tags e2eplacement ./e2e/placement/...
	$(GO) vet -tags e2elocks ./e2e/locks/...
	$(GO) vet -tags e2equeues ./e2e/queues/...
	$(GO) vet -tags e2eobs ./e2e/observability/...
	$(GO) vet -tags e2emultimgmt ./e2e/multimgmt/...
	@sh scripts/check-secrets.sh

# The lint tools come from tools/go.mod (a separate module, so their
# dependencies never touch the main build) and are built into bin/lint/.
.PHONY: lint-tools
lint-tools:
	$(GO) build -C tools -o $(CURDIR)/bin/lint/ honnef.co/go/tools/cmd/staticcheck golang.org/x/vuln/cmd/govulncheck

# staticcheck: linux and darwin, the same on a Mac as in CI (how: the
# script's header). Silence a finding only with a
# `//lint:ignore <check> <reason>` comment on the line above it.
.PHONY: staticcheck
staticcheck: lint-tools
	GO=$(GO) sh scripts/staticcheck.sh bin/lint/staticcheck

# vulncheck: govulncheck ./... against the Go vulnerability database
# (vuln.go.dev, so it needs the network). It fails on any vulnerability the
# code can reach, and `make lint` fails with it; see "Upstream advisories" in
# docs/RELEASING.md.
.PHONY: vulncheck
vulncheck: lint-tools
	bin/lint/govulncheck ./...

.PHONY: hooks
hooks:
	sh scripts/install-hooks.sh

.PHONY: clean
clean:
	rm -rf bin dist

## gen-js: rebuild the runtime module hostd embeds into every deployment (from @tailworks/tailnet).
.PHONY: gen-js
gen-js: $(WORKERD)
	node_modules/.bin/esbuild js/packages/tailnet/src/runtime.ts --bundle --format=esm --platform=neutral \
		--target=es2022 --legal-comments=none --log-level=warning --external:node:async_hooks --outfile=hostd/gen/js/tw-rt.js

## test-vitest: templates under @cloudflare/vitest-plugin with @tailworks/tailnet/testing (docs/testing.md;
## npm ci of the pinned deps in e2e/vitest, which bring their own workerd via miniflare).
.PHONY: test-vitest
test-vitest: deps
	cd e2e/vitest && npm ci --no-audit --no-fund --loglevel=error
	cd e2e/vitest && npx vitest run

## test-node: the opt-in Node process tier + Vercel Workflow (hermetic; npm ci of the pinned deps in e2e/node).
.PHONY: test-node
test-node: deps
	cd e2e/node && npm ci --no-audit --no-fund --loglevel=error
	npm test -w @tailworks/node --silent
	$(GO) test -tags nodetier -count=1 -v -timeout 10m ./e2e/node/...
