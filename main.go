package main

import (
	"github.com/IBM-tfproviders/terraform-provider-vsphere/vsphere"
	"github.com/hashicorp/terraform/plugin"
)

func main() {
	printBuildVersion()
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: vsphere.Provider,
	})
}
