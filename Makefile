# Package
GO_PKG := github.com/coreos-inc/kube-chargeback

# Hive Git repository for Thrift definitions
HIVE_REPO := "git://git.apache.org/hive.git"
HIVE_SHA := "1fe8db618a7bbc09e041844021a2711c89355995"

# TODO: Add tests
all: fmt chargeback-docker-build

docker-build: chargeback-docker-build promsum-docker-build presto-docker-build hive-docker-build

docker-push: chargeback-docker-push promsum-docker-push presto-docker-push hive-docker-push

dist: Documentation manifests examples hack/*.sh
	mkdir -p $@
	cp -r $? $@

dist.zip: dist
	zip -r $@ $?

promsum-docker-build: images/promsum/IMAGE images/promsum/bin/promsum
	docker build $(BUILD_ARGS) -t $$(cat $<) $(dir $<)

promsum-docker-push: images/promsum/IMAGE
	docker push $$(cat $<)

chargeback-docker-build: images/chargeback/IMAGE images/chargeback/bin/chargeback
	docker build $(BUILD_ARGS) -t $$(cat $<) $(dir $<)

chargeback-docker-push: images/chargeback/IMAGE
	docker push $$(cat $<)

presto-docker-build: images/presto/IMAGE
	docker build -t $$(cat $<) $(dir $<)

presto-docker-push: images/presto/IMAGE
	docker push $$(cat $<)

hive-docker-build: images/hive/IMAGE
	docker build -t $$(cat $<) $(dir $<)

hive-docker-push: images/hive/IMAGE
	docker push $$(cat $<)

# Update dependencies
vendor: glide.yaml
	glide up --strip-vendor
	glide-vc --use-lock-file --no-tests --only-code

# Runs gofmt on all files in project except vendored source and Hive Thrift definitions
fmt:
	find . -name '*.go' -not -path "./vendor/*" -not -path "./pkg/hive/hive_thrift/*" | xargs gofmt -s -w

images/chargeback/bin/chargeback: cmd/chargeback pkg/hive/hive_thrift
	mkdir -p $(dir $@)
	GOOS=linux go build -i -v -o $@ ${GO_PKG}/$<

images/promsum/bin/promsum: cmd/promsum
	mkdir -p $(dir $@)
	GOOS=linux go build -i -v -o $@ ${GO_PKG}/$<

# Download Hive git repo.
out/thrift.git:
	mkdir -p out
	git clone --single-branch --bare ${HIVE_REPO} $@

# Retrieve Hive thrift definition from git repo.
thrift/TCLIService.thrift: out/thrift.git
	mkdir -p $(dir $@)
	git -C $< show ${HIVE_SHA}:service-rpc/if/$(notdir $@) > $@

# Generate source from Hive thrift defintions and remove executable packages.
pkg/hive/hive_thrift: thrift/TCLIService.thrift
	thrift -gen go:package_prefix=${GO_PKG}/$(dir $@),package=$(notdir $@) -out $(dir $@) $<
	for i in `go list -f '{{if eq .Name "main"}}{{ .Dir }}{{end}}' ./$@/...`; do rm -rf $$i; done

.PHONY: vendor fmt chargeback-docker-build promsum-docker-build presto-docker-build hive-docker-build chargeback-docker-push promsum-docker-push presto-docker-push hive-docker-push docker-build docker-push
