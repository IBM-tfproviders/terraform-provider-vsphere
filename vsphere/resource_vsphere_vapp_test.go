package vsphere

import (
	//"fmt"
	//"log"
	"os"
	"testing"

	/*
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
	*/
)

func TestAccVSphereVapp_validatorFunc(t *testing.T) {
	var validatorCases = []attributeValueValidationTestSpec{
		{name: "type", validatorFn: validateEntityType,
			values: []attributeProperty{
				{value: "vm", successCase: true},
				{value: "vapp", successCase: true},
				{value: "" , expErr: "Supported values are"},
				{value: "VirtualMachine" , expErr: "Supported values are"},
				{value: "VirtualApp" , expErr: "Supported values are"},
			},
		},
		{name: "start_action", validatorFn: validateStartAction,
			values: []attributeProperty{
				{value: "none", successCase: true},
				{value: "powerOn", successCase: true},
			},
		},
		{name: "stop_action", validatorFn: validateStopAction,
			values: []attributeProperty{
				{value: "none", successCase: true},
				{value: "powerOff", successCase: true},
			},
		},
	}

	verifySchemaValidationFunctions(t, validatorCases)
}

func testAccPreCheckVapp(t *testing.T) {

	var envList = []string{"VSPHERE_DATACENTER"}

	testAccPreCheck(t)

	for _, env := range envList {
		if v := os.Getenv(env); v == "" {
			t.Fatal(env + " must be set for acceptance tests")
		}
	}
}
