package main

import (
	"log"
)

var Version string
var ProviderName = "PROVIDER"

func printBuildVersion() {
	if Version == "" {
		Version = "DEV"
	}
	log.Printf("[INFO] %s.Version: %s", ProviderName, Version)
}
