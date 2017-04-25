package vsphere

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

var PortgroupTypes = []string{
	string(types.DistributedVirtualPortgroupPortgroupTypeEarlyBinding),
	string(types.DistributedVirtualPortgroupPortgroupTypeLateBinding),
	string(types.DistributedVirtualPortgroupPortgroupTypeEphemeral),
}

const (
	PortgroupVlanTypeNone     = "none"
	PortgroupVlanTypeVlan     = "vlanid"
	PortgroupVlanTypePVid     = "pvid"
	PortgroupVlanTypeTrunking = "trunking"
)

var VlanType = []string{
	string(PortgroupVlanTypeNone),
	string(PortgroupVlanTypeVlan),
	string(PortgroupVlanTypePVid),
	string(PortgroupVlanTypeTrunking),
}

type pgVlan struct {
	vlan_type  string
	vlan_id    int32
	vlan_range []types.NumericRange
}

type vdPortgroup struct {
	datacenter     string
	vds_name       string
	portgroup_name string
	portgroup_type string
	description    string
	num_ports      int32
	pgVlan
}

func resourceVSphereVdPortgroup() *schema.Resource {
	return &schema.Resource{
		Create: resourceVSphereVdPortgroupCreate,
		Read:   resourceVSphereVdPortgroupRead,
		Update: resourceVSphereVdPortgroupUpdate,
		Delete: resourceVSphereVdPortgroupDelete,

		Schema: map[string]*schema.Schema{
			"datacenter": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"vds_name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"portgroup_name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},

			"portgroup_type": &schema.Schema{
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "earlyBinding",
				ValidateFunc: validatePortgroupType,
			},

			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Default:  "Created by Terraform",
			},

			"num_ports": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  8,
			},
			"vlan": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"type": &schema.Schema{
							Type:         schema.TypeString,
							Optional:     true,
							Default:      "none",
							ValidateFunc: validateVlanType,
						},
						"vlan_id": &schema.Schema{
							Type:     schema.TypeInt,
							Optional: true,
						},
						"vlan_range": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
			},
		},
	}
}

func resourceVSphereVdPortgroupCreate(d *schema.ResourceData, meta interface{}) error {

	//client := meta.(*govmomi.Client)
	pg, err := NewVdPortgroup(d)
	if err != nil {
		log.Printf("[DEBUG] NewVdPortgroup failed.")
		return err
	}

	err = validatePortgroupConfigs(pg)
	if err != nil {
		log.Printf("[DEBUG] Configuration validation failed.")
		return err
	}
	log.Printf("[DEBUG] creating vDs portgroup: %s", pg)

	netRef, err := findVds(pg, meta)
	if err != nil {
		return err
	}
	vDS := netRef.(*object.DistributedVirtualSwitch)

	pgSpec := types.DVPortgroupConfigSpec{
		Description: pg.description,
		Name:        pg.portgroup_name,
		Type:        pg.portgroup_type,
		NumPorts:    pg.num_ports,
	}

	portSettings := new(types.VMwareDVSPortSetting)

	switch pg.vlan_type {
	case PortgroupVlanTypeVlan:
		vlanCnf := new(types.VmwareDistributedVirtualSwitchVlanIdSpec)
		vlanCnf.VlanId = pg.vlan_id
		portSettings.Vlan = vlanCnf

	case PortgroupVlanTypePVid:
		vlanCnf := new(types.VmwareDistributedVirtualSwitchPvlanSpec)
		vlanCnf.PvlanId = pg.vlan_id
		portSettings.Vlan = vlanCnf

	case PortgroupVlanTypeTrunking:
		vlanCnf := new(types.VmwareDistributedVirtualSwitchTrunkVlanSpec)
		vlanCnf.VlanId = pg.vlan_range
		portSettings.Vlan = vlanCnf

	// Nothing to do
	case "none":
	default:
	}

	pgSpec.DefaultPortConfig = portSettings

	// Now call AddPortgroup API
	//
	task, err := vDS.AddPortgroup(context.TODO(), []types.DVPortgroupConfigSpec{pgSpec})
	if err != nil {
		return nil
	}
	_, err = task.WaitForResult(context.TODO(), nil)
	if err != nil {
		return err
	}

	d.SetId(fmt.Sprintf("%s/network/%s", pg.datacenter, pg.portgroup_name))
	return resourceVSphereVdPortgroupRead(d, meta)
}

func resourceVSphereVdPortgroupRead(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*govmomi.Client)
	pg_name := d.Get("portgroup_name").(string)
	log.Printf("[DEBUG] reading vDs portgroup: [%s - %s]",
		pg_name, d.Id())

	pg, err := object.NewSearchIndex(client.Client).FindByInventoryPath(
		context.TODO(), d.Id())

	if err != nil {
		return err
	}
	if pg == nil {
		d.SetId("")
		return fmt.Errorf("portgroup '%s' not found", pg_name)
	}

	log.Printf("[DEBUG] The vDS portgroup : %#v", pg)
	return nil
}

func resourceVSphereVdPortgroupUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Updating vDs portgroup: %s", d.Get("portgroup_name").(string))
	//client := meta.(*govmomi.Client)
    return nil
}

func resourceVSphereVdPortgroupDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Deleting vDs portgroup: %s", d.Get("portgroup_name").(string))
	//client := meta.(*govmomi.Client)
    return nil
}

func findVds(pg *vdPortgroup, meta interface{}) (object.NetworkReference, error) {
	client := meta.(*govmomi.Client)
	dc, err := getDatacenter(client, pg.datacenter)
	if err != nil {
		return nil, err
	}

	finder := find.NewFinder(client.Client, true)
	finder = finder.SetDatacenter(dc)

	vDS, err := finder.Network(context.TODO(), pg.vds_name)
	if err != nil {
		return nil, err
	}
	log.Printf("[DEBUG] The vDS : %#v", vDS)

	return vDS, nil
}

func NewVdPortgroup(d *schema.ResourceData) (*vdPortgroup, error) {
	pg := &vdPortgroup{
		vds_name:       d.Get("vds_name").(string),
		portgroup_name: d.Get("portgroup_name").(string),
	}

	if v, ok := d.GetOk("datacenter"); ok {
		pg.datacenter = v.(string)
	}

	if v, ok := d.GetOk("portgroup_type"); ok {
		pg.portgroup_type = v.(string)
	}

	if v, ok := d.GetOk("description"); ok {
		pg.description = v.(string)
	}

	if v, ok := d.GetOk("num_ports"); ok {
		pg.num_ports = int32(v.(int))
	}

	if vL, ok := d.GetOk("vlan"); ok {
		var vlancfg pgVlan
		vlan_infos := (vL.([]interface{}))[0].(map[string]interface{})

		if v, ok := vlan_infos["type"].(string); ok && v != "" {
			vlancfg.vlan_type = v
		}
		if v, ok := vlan_infos["vlan_id"].(int); ok {
			vlancfg.vlan_id = int32(v)
		}

		if v, ok := vlan_infos["vlan_range"].(string); ok && v != "" {
			vlan_range, err := parseVlanRange(string(v))
			if err != nil {
				log.Printf("[DEBUG] Vlan range parsing failed.")
				return nil, err
			} else {
				vlancfg.vlan_range = vlan_range
			}
		}

		pg.pgVlan = vlancfg
	}

	return pg, nil
}

func parseVlanRange(vlan_range string) (result []types.NumericRange, errors error) {

	vlans := strings.Split(vlan_range, ",")

	for _, v := range vlans {

		var numRange types.NumericRange

		match, _ := regexp.MatchString("^(\\d+)-(\\d+)$", strings.TrimSpace(v))

		if match {
			vlan := strings.Split(strings.TrimSpace(v), "-")
			start, _ := strconv.Atoi(vlan[0])
			end, _ := strconv.Atoi(vlan[1])
			numRange = types.NumericRange{Start: int32(start), End: int32(end)}
		} else {
			match, _ = regexp.MatchString("^\\d+$", strings.TrimSpace(v))
			if match {
				start, _ := strconv.Atoi(strings.TrimSpace(v))
				numRange = types.NumericRange{Start: int32(start), End: int32(start)}
			} else {
				return nil, fmt.Errorf("vlan range '%s' is not valid.", vlan_range)
			}
		}
		result = append(result, numRange)
	}

	return result, nil
}

func validatePortgroupType(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	found := false

	for _, t := range PortgroupTypes {
		if t == value {
			found = true
		}
	}
	if !found {
		errors = append(errors, fmt.Errorf(
			"%s: Supported values are %s", k, strings.Join(PortgroupTypes, ", ")))
	}

	return
}

func validateVlanType(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	found := false

	for _, t := range VlanType {
		if t == value {
			found = true
		}
	}
	if !found {
		errors = append(errors, fmt.Errorf(
			"%s: Supported values are %s", k, strings.Join(VlanType, ", ")))
	}

	return
}

func validatePortgroupConfigs(pg *vdPortgroup) error {
	if (pg.vlan_type == PortgroupVlanTypeVlan ||
		pg.vlan_type == PortgroupVlanTypePVid) &&
		(pg.vlan_id == 0) {
		return fmt.Errorf("vlan id is not configured for the type '%s'",
			pg.vlan_type)
	} else if pg.vlan_type == PortgroupVlanTypeTrunking {
		if len(pg.vlan_range) == 0 {
			return fmt.Errorf("vlan range is not configured for the type '%s'",
				pg.vlan_type)
		} else {
			for _, v := range pg.vlan_range {
				if v.End != 0 && v.Start > v.End {
					return fmt.Errorf("vlan range '%s' is not valid ",
						pg.vlan_range)
				}
			}
		}
	}
	return nil
}
