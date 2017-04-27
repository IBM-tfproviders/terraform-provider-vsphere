package main

import (
	"github.com/IBM-tfproviders/vmware-vsphere/vsphere"
	"github.com/hashicorp/terraform/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: vsphere.Provider,
	})
}
