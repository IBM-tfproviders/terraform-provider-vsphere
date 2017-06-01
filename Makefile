PKG_LIST=$(shell go list ./...)
PROVIDER_NAME=terraform-provider-vsphere
GIT_TAG=$(shell git describe --always --long --dirty)
LD_FLAGS += " -X main.Version=${GIT_TAG} -X main.ProviderName=${PROVIDER_NAME}"

deps:
	go get github.com/hashicorp/terraform
	go get github.com/vmware/govmomi
	go get golang.org/x/net/context
	
build:
	go build -ldflags ${LD_FLAGS} -o $(PROVIDER_NAME) github.com/IBM-tfproviders/terraform-provider-vsphere

all: deps build

testacc:
	@echo "Starting Acceptance Test..."
	TF_ACC=1 go test ./vsphere -v $(TESTARGS) -timeout 120m

fmt:
	@echo "Running 'go fmt'..."
	go fmt $(PKG_LIST)

clean:
	rm -f terraform-provider-vsphere

.PHONY: build 
