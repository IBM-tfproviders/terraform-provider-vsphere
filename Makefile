deps:
	go get github.com/hashicorp/terraform
	go get github.com/vmware/govmomi
	go get golang.org/x/net/context
	
build:
	go build -o terraform-provider-vsphere main.go

all: deps build

testacc:
	@echo "Starting Acceptance Test..."
	TF_ACC=1 go test ./vsphere -v $(TESTARGS) -timeout 120m

fmt:
	@echo "Running 'go fmt'..."
	go fmt ./vsphere

.PHONY: build 
