package main

import (
	"log"
)

var Version string = "DEV"
var ProviderName = "PROVIDER"

func printBuildVersion() {
	log.Printf("[INFO] %s.Version: [%s].", ProviderName, Version)
}
