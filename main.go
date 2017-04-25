package main

import (
	"github.com/IBM-tfproviders/vmware-vsphere/provider_vsphere"
	"github.com/hashicorp/terraform/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: vsphere.Provider,
	})
}
