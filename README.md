# Terraform provider for vsphere
## About this repository

This repository created by filtering the subdirectory https://github.com/hashicorp/terraform/tree/master/builtin/providers/vsphere
from Git repo https://github.com/hashicorp/terraform

Original [README](https://github.com/hashicorp/terraform/blob/master/builtin/providers/vsphere/README.md)


## How to build the teraform provider for vsphere.

1. Export GOPATH and append PATH with $GOPATH/bin
2. Clone or checkout this repository at $GOPATH/src/github.com/IBM-tfproviders
3. cd to $GOPATH/src/github.com/IBM-tfproviders/vmware-vsphere
4. make deps
5. make build
