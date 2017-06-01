package vsphere

import (
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
	//"github.com/vmware/govmomi"
	//"github.com/vmware/govmomi/find"
	//"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	//"golang.org/x/net/context"
)

var (
	pgDatacenter = os.Getenv("VSPHERE_DATACENTER")
	pgVdsName    = os.Getenv("VSPHERE_VDS_NAME")
)

const (
	testAccCheckVdsConf_min = `
resource "vsphere_vds_portgroup" "%s" {
    portgroup_name = "%s"
    datacenter = "%s"
    vds_name = "%s"
}
`
	testAccCheckVdsConf = `
resource "vsphere_vds_portgroup" "%s" {
    portgroup_name = "%s"
    datacenter = "%s"
    vds_name = "%s"
    portgroup_type = "%s"
    description = "%s"
    num_ports = "%d"
}
`
)

// Verify default values with minimum configuration
//
func TestAccVSphereVdsPortgroupUpdate(t *testing.T) {
	pgName := "TFT_pg1"
	resourceName := "vsphere_vds_portgroup." + pgName
	defPortgroupType := string(types.DistributedVirtualPortgroupPortgroupTypeEarlyBinding)

	config := fmt.Sprintf(testAccCheckVdsConf_min, pgName, pgName, pgDatacenter,
		pgVdsName)
	log.Printf("[DEBUG] template config= %s", config)

	configUpdate := fmt.Sprintf(testAccCheckVdsConf, pgName, pgName, pgDatacenter,
		pgVdsName, defPortgroupType, "Updated by Terraform", 16)
	log.Printf("[DEBUG] template configUpdate= %s", configUpdate)

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheckVdsPg(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckVdsPortGroupDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						resourceName, "portgroup_type", defPortgroupType),
					resource.TestCheckResourceAttr(
						resourceName, "description", "Created by Terraform"),
				),
			},
			resource.TestStep{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						resourceName, "portgroup_type", defPortgroupType),
					resource.TestCheckResourceAttr(
						resourceName, "description", "Updated by Terraform"),
					resource.TestCheckResourceAttr(
						resourceName, "num_ports", "16"),
				),
			},
		},
	})
}

func testAccCheckVdsPortGroupDestroy(s *terraform.State) error {
	//client := testAccProvider.Meta().(*govmomi.Client)
	//finder := find.NewFinder(client.Client, true)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "vsphere_vds_portgroup" {
			continue
		}
	}

	return nil

}

type vdsPgInput struct {
	successCase bool
	value       interface{}
	expErr      string
	expWarn     string
}

type vdsPgFnValidationObj struct {
	paramName   string
	validatorFn schema.SchemaValidateFunc
	inoutList   []vdsPgInput
}

func TestAccVSphereVdsPortgroup_validatorFunc(t *testing.T) {
	var validatorCases = []vdsPgFnValidationObj{
		{paramName: "num_ports", validatorFn: validateNumPorts,
			inoutList: []vdsPgInput{
				{value: -1, expErr: "out of allowed range"},
				{value: 0, successCase: true},
				{value: 212, successCase: true},
				{value: 8193, expErr: "out of allowed range"},
			},
		},
		{paramName: "portgroup_type", validatorFn: validatePortgroupType,
			inoutList: []vdsPgInput{
				{value: "Unknown", expErr: "Supported values are"},
				{value: string(types.DistributedVirtualPortgroupPortgroupTypeEarlyBinding), successCase: true},
			},
		},
		{paramName: "vlan_id", validatorFn: validateVlanId,
			inoutList: []vdsPgInput{
				{value: 4095, expErr: "is out of range"},
				{value: -2, expErr: "is out of range"},
				{value: 1, successCase: true},
				{value: 4094, successCase: true},
			},
		},
		{paramName: "vlan_range", validatorFn: validateVlanRange,
			inoutList: []vdsPgInput{
				{value: "a0jomadfadsf", expErr: "is in incorrect format"},
				{value: "1-o", expErr: "is in incorrect format"},
				{value: "10-20,A", expErr: "is in incorrect format"},
				{value: "10-20-30", expErr: "is in incorrect format"},
				{value: "10-5030", expErr: "is out of range"},
				{value: "5030", expErr: "is out of range"},
				{value: "530-10", expErr: "needs to be smaller than"},
				{value: "1234", successCase: true},
				{value: "123-234", successCase: true},
				{value: "12-34,,5-6", successCase: true},
				{value: "12-34,56,78-91", successCase: true},
			},
		},
		{paramName: "type", validatorFn: validateVlanType,
			inoutList: []vdsPgInput{
				{value: "Unknown", expErr: "Supported values are"},
				{value: string(portgroupVlanTypeNone), successCase: true},
				{value: string(portgroupVlanTypePVid), successCase: true},
			},
		},
	}

	for _, c := range validatorCases {

		log.Printf("* Executing validator function for parameter: '%s'", c.paramName)

		for _, inout := range c.inoutList {
			var warns []string
			var errors []error

			log.Printf("Validating:> parameter:'%s' value:'%v'", c.paramName, inout.value)

			warns, errors = c.validatorFn(inout.value, c.paramName)
			// log.Printf("warns %s - errors %s", warns, errors)

			if inout.successCase {
				if len(errors) > 0 || len(warns) > 0 {
					t.Fatalf("ParamValidationFailed: param '%s' value '%v' is not VALID.",
						c.paramName, inout.value)
				}

			} else {
				if errors != nil {
					ok := strings.Contains(errors[0].Error(), inout.expErr)
					if !ok {
						t.Fatalf("ParamValidationFailed: '%s'. Expected ERROR '%v' not found.",
							c.paramName, inout.expErr)
					}
				} else if warns != nil {
					ok := strings.Contains(warns[0], inout.expErr)
					if !ok {
						t.Fatalf("ParamValidationFailed: '%s'. Expected WARNING '%v' not found.",
							c.paramName, inout.expErr)
					}
				}
			}
		}
	}
}

func testAccPreCheckVdsPg(t *testing.T) {

	var envList = []string{"VSPHERE_DATACENTER", "VSPHERE_VDS_NAME"}

	testAccPreCheck(t)

	for _, env := range envList {
		if v := os.Getenv(env); v == "" {
			t.Fatal(env + " must be set for acceptance tests")
		}
	}
}
