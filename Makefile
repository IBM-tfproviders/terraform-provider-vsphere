deps:
	go get github.com/hashicorp/terraform
	go get github.com/vmware/govmomi
	go get golang.org/x/net/context
	
build:
	go build -o terraform-provider-vsphere main.go

all: deps build

.PHONY: build 
