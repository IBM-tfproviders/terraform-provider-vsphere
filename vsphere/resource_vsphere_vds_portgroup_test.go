package vsphere

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	//"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

var (
	pgDatacenter = os.Getenv("VSPHERE_DATACENTER")
	pgVdsName    = os.Getenv("VSPHERE_VDS_NAME")
)

const (
	defPortgroupType = string(types.DistributedVirtualPortgroupPortgroupTypeEarlyBinding)

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
func TestAccVSphereVdsPortgroup_DefaultValues(t *testing.T) {
	pgName := "TFT_DEFAULT"
	resourceName := "vsphere_vds_portgroup." + pgName

	config := fmt.Sprintf(testAccCheckVdsConf_min, pgName, pgName, pgDatacenter,
		pgVdsName)
	log.Printf("[DEBUG] template config= %s", config)

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
					resource.TestCheckResourceAttr(
						resourceName, "num_ports", "8"),
				),
			},
		},
	})
}

// Verify update operation.
//
func TestAccVSphereVdsPortgroup_UpdateOperation(t *testing.T) {
	pgName := "TFT_UPDATE"
	resourceName := "vsphere_vds_portgroup." + pgName

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
	client := testAccProvider.Meta().(*govmomi.Client)
	finder := find.NewFinder(client.Client, true)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "vsphere_vds_portgroup" {
			continue
		}

		dc, err := finder.Datacenter(context.TODO(), rs.Primary.Attributes["datacenter"])
		if err != nil {
			return fmt.Errorf("error %s", err)
		}

		finder = finder.SetDatacenter(dc)

		pgName := rs.Primary.Attributes["portgroup_name"]
		netRef, netErr := finder.Network(context.TODO(), pgName)
		if netErr != nil {
			switch e := netErr.(type) {
			case *find.NotFoundError:
				fmt.Printf("Expected error received: %s\n", e.Error())
				return nil
			default:
				fmt.Printf("finder.Network RETURNS:> netRef=%#v | e=%#v\n", netRef, e)
				return netErr
			}
		} else {
			if netRef != nil {
				return fmt.Errorf("portgroup %s still exists", pgName)
			} else {
				log.Printf("portgroup %s already deleted.", pgName)
				return nil
			}
		}
	}

	return nil
}

func TestAccVSphereVdsPortgroup_validatorFunc(t *testing.T) {
	var validatorCases = []attributeValueValidationTestSpec{
		{name: "num_ports", validatorFn: validateNumPorts,
			values: []attributeProperty{
				{value: -1, expErr: "out of allowed range"},
				{value: 0, successCase: true},
				{value: 212, successCase: true},
				{value: 8193, expErr: "out of allowed range"},
			},
		},
		{name: "portgroup_type", validatorFn: validatePortgroupType,
			values: []attributeProperty{
				{value: "Unknown", expErr: "Supported values are"},
				{value: string(types.DistributedVirtualPortgroupPortgroupTypeEarlyBinding), successCase: true},
			},
		},
		{name: "vlan_id", validatorFn: validateVlanId,
			values: []attributeProperty{
				{value: 4095, expErr: "is out of range"},
				{value: -2, expErr: "is out of range"},
				{value: 1, successCase: true},
				{value: 4094, successCase: true},
			},
		},
		{name: "vlan_range", validatorFn: validateVlanRange,
			values: []attributeProperty{
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
		{name: "type", validatorFn: validateVlanType,
			values: []attributeProperty{
				{value: "Unknown", expErr: "Supported values are"},
				{value: string(portgroupVlanTypeNone), successCase: true},
				{value: string(portgroupVlanTypePVid), successCase: true},
			},
		},
	}

	verifySchemaValidationFunctions(t, validatorCases)
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
