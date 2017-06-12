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
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

var portgroupTypesList = []string{
	string(types.DistributedVirtualPortgroupPortgroupTypeEarlyBinding),
	string(types.DistributedVirtualPortgroupPortgroupTypeLateBinding),
	string(types.DistributedVirtualPortgroupPortgroupTypeEphemeral),
}

const (
	portgroupVlanTypeNone     = "none"
	portgroupVlanTypeVlan     = "vlan"
	portgroupVlanTypePVid     = "pvlan"
	portgroupVlanTypeTrunking = "trunking"

	vlanIdMin = 1
	vlanIdMax = 4094

	portgroupNumPortsMin     = 0
	portgroupNumPortsMax     = 8192
	portgroupNumPortsDefault = 8

	pgInventoryPath = "%s/network/%s"
)

var vlanTypeList = []string{
	string(portgroupVlanTypeNone),
	string(portgroupVlanTypeVlan),
	string(portgroupVlanTypePVid),
	string(portgroupVlanTypeTrunking),
}

type pgVlan struct {
	vlanType  string
	vlanId    int32
	vlanRange []types.NumericRange
}

type vdPortgroup struct {
	datacenter    string
	vdsName       string
	portgroupName string
	portgroupType string
	description   string
	numPorts      int32
	pgVlan
}

func resourceVSphereVdPortgroup() *schema.Resource {
	return &schema.Resource{
		Create: resourceVSphereVdPortgroupCreate,
		Read:   resourceVSphereVdPortgroupRead,
		Update: resourceVSphereVdPortgroupUpdate,
		Delete: resourceVSphereVdPortgroupDelete,

		SchemaVersion: 1,

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
				Default:      types.DistributedVirtualPortgroupPortgroupTypeEarlyBinding,
				ValidateFunc: validatePortgroupType,
			},

			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Default:  "Created by Terraform",
			},

			"num_ports": &schema.Schema{
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      portgroupNumPortsDefault,
				ValidateFunc: validateNumPorts,
			},
			"vlan": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"type": &schema.Schema{
							Type:         schema.TypeString,
							Optional:     true,
							Default:      portgroupVlanTypeNone,
							ValidateFunc: validateVlanType,
						},
						"vlan_id": &schema.Schema{
							Type:         schema.TypeInt,
							Optional:     true,
							ValidateFunc: validateVlanId,
						},
						"vlan_range": &schema.Schema{
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validateVlanRange,
						},
					},
				},
			},
		},
	}
}

func resourceVSphereVdPortgroupCreate(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*govmomi.Client)
	pg, _ := parsePortgroupData(d)

	if err := validatePortgroupConfigs(pg); err != nil {
		log.Printf("[ERROR] Configuration validation failed.")
		return err
	}
	log.Printf("[INFO] creating vDS portgroup: %#v", pg)

	vdsRef, err := findNetObjectByName(pg.datacenter, pg.vdsName, client)
	if err != nil {
		return err
	}
	vDS := vdsRef.(*object.DistributedVirtualSwitch)

	pgSpec := types.DVPortgroupConfigSpec{
		Description: pg.description,
		Name:        pg.portgroupName,
		Type:        pg.portgroupType,
		NumPorts:    pg.numPorts,
	}

	pgSpec.DefaultPortConfig = setPortSettings(pg.pgVlan)

	// Now call AddPortgroup API
	//
	task, err := vDS.AddPortgroup(context.TODO(), []types.DVPortgroupConfigSpec{pgSpec})
	if err != nil {
		return err
	}
	_, err = task.WaitForResult(context.TODO(), nil)
	if err != nil {
		return err
	}

	// Find the newly created object and set required fields.
	//
	netRef, err = findNetObjectByName(pg.datacenter, pg.portgroupName, client)
	dvsPortGrp := netRef.(*object.DistributedVirtualPortgroup)
	d.SetId(dvsPortGrp.InventoryPath)

	if pg.datacenter == "" {
		dcName := strings.Split(dvsPortGrp.InventoryPath, "/")[0]
		log.Printf("[INFO] Retrieve DC '%s' from inventory path %s",
			dcName, dvsPortGrp.InventoryPath)
		d.Set("datacenter", dcName)
	}

	return resourceVSphereVdPortgroupRead(d, meta)
}

func resourceVSphereVdPortgroupRead(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*govmomi.Client)
	dcName := d.Get("datacenter").(string)
	pgName := d.Get("portgroup_name").(string)

	log.Printf("[INFO] reading vDS portgroup: [%s]", d.Id())

	netRef, err := findNetObjectByName(dcName, pgName, client)
	if err != nil {
		return err
	}
	if netRef == nil {
		d.SetId("")
		return fmt.Errorf("portgroup '%s' not found in vDS %s in datacenter %s.",
			pgName, d.Get("vds_name").(string), dcName)
	}

	log.Printf("[DEBUG] The vDS Portgroup : %#v", netRef)
	return nil
}

func resourceVSphereVdPortgroupUpdate(d *schema.ResourceData, meta interface{}) error {

	pg, _ := parsePortgroupData(d)

	if err := validatePortgroupConfigs(pg); err != nil {
		log.Printf("[ERROR] Configuration validation failed.")
		return err
	}

	pgName := pg.portgroupName
	pgSpec := types.DVPortgroupConfigSpec{}

	if d.HasChange("portgroup_name") {
		oldpg, _ := d.GetChange("portgroup_name")
		pgName = oldpg.(string)
		pgSpec.Name = pg.portgroupName
	}
	log.Printf("[INFO] Updating vDS portgroup: %s", pgName)

	client := meta.(*govmomi.Client)
	netRef, err := findNetObjectByName(pg.datacenter, pgName, client)
	if err != nil {
		log.Printf("[ERROR] PortGroup '%s' object not found for update", pgName)
		return err
	}

	if d.HasChange("portgroup_type") {
		pgSpec.Type = pg.portgroupType
	}

	if d.HasChange("description") {
		pgSpec.Description = pg.description
	}

	if d.HasChange("num_ports") {
		pgSpec.NumPorts = pg.numPorts
	}

	if d.HasChange("vlan") {
		vlancfg := parseVlan(d)
		pgSpec.DefaultPortConfig = setPortSettings(vlancfg)
	}

	dvsPortGrp := netRef.(*object.DistributedVirtualPortgroup)

	var mopg mo.DistributedVirtualPortgroup
	err = dvsPortGrp.Properties(context.TODO(), dvsPortGrp.Reference(),
		[]string{"config.configVersion"}, &mopg)
	if err != nil {
		return err
	}

	pgSpec.ConfigVersion = mopg.Config.ConfigVersion

	task, err := dvsPortGrp.Reconfigure(context.TODO(), pgSpec)
	if err != nil {
		return err
	}

	_, err = task.WaitForResult(context.TODO(), nil)
	if err != nil {
		log.Printf("[ERROR] Portgroup %s updation failed.", pgName)
		return err
	}

	if d.HasChange("portgroup_name") {
		// Find the newly created object and set required fields.
		//
		netRef, err = findNetObjectByName(pg.datacenter, pg.portgroupName, client)
		if netRef == nil || err != nil {
			return fmt.Errorf("portgroup '%s' update is not complete.",
				pg.portgroupName)
		}

		dvsPortGrp = netRef.(*object.DistributedVirtualPortgroup)
		d.SetId(dvsPortGrp.InventoryPath)
	}

	return nil
}

func resourceVSphereVdPortgroupDelete(d *schema.ResourceData, meta interface{}) error {

	dcName := d.Get("datacenter").(string)
	pgName := d.Get("portgroup_name").(string)

	log.Printf("[INFO] Deleting vDS portgroup: %s", pgName)

	client := meta.(*govmomi.Client)
	netRef, err := findNetObjectByName(dcName, pgName, client)
	if err != nil {
		return err
	}

	dvsPortGrp := netRef.(*object.DistributedVirtualPortgroup)

	task, err := dvsPortGrp.Destroy(context.TODO())
	if err != nil {
		return err
	}
	_, err = task.WaitForResult(context.TODO(), nil)
	if err != nil {
		log.Printf("[ERROR] Portgroup %s deletion failed.", pgName)
		return err
	}

	return nil
}

func findNetObjectByName(dcName string, netName string,
	client *govmomi.Client) (object.NetworkReference, error) {

	log.Printf("[DEBUG] Finding network %s object in datacenter %s", netName, dcName)
	dc, err := getDatacenter(client, dcName)
	if err != nil {
		log.Printf("[ERROR] datacenter '%s' not found", dcName)
		return nil, err
	}

	finder := find.NewFinder(client.Client, true)
	finder = finder.SetDatacenter(dc)

	netRef, err := finder.Network(context.TODO(), netName)
	if err != nil {
		log.Printf("[ERROR] Network '%s' object not found in datacenter %s.",
			netName, dcName)
		return nil, err
	}
	log.Printf("[DEBUG] Network reference : %#v", netRef)

	return netRef, nil
}

func findVdsPgByInventoryPath(d *schema.ResourceData, meta interface{}) (object.Reference, error) {
	client := meta.(*govmomi.Client)
	pgName := d.Get("portgroup_name").(string)

	pgRef, err := object.NewSearchIndex(client.Client).FindByInventoryPath(
		context.TODO(), d.Id())
	if err != nil {
		log.Printf("[ERROR] portgroup '%s' search failed.", pgName)
		return nil, err
	}
	if pgRef == nil {
		return nil, fmt.Errorf("portgroup '%s' not found in vDS %s in datacenter %s.",
			pgName, d.Get("vds_name").(string), d.Get("datacenter").(string))
	}

	return pgRef, nil
}

func parsePortgroupData(d *schema.ResourceData) (*vdPortgroup, error) {
	pg := &vdPortgroup{
		vdsName:       d.Get("vds_name").(string),
		portgroupName: d.Get("portgroup_name").(string),
	}

	if v, ok := d.GetOk("datacenter"); ok {
		pg.datacenter = v.(string)
	}

	if v, ok := d.GetOk("portgroup_type"); ok {
		pg.portgroupType = v.(string)
	}

	if v, ok := d.GetOk("description"); ok {
		pg.description = v.(string)
	}

	if v, ok := d.GetOk("num_ports"); ok {
		pg.numPorts = int32(v.(int))
	}

	pg.pgVlan = parseVlan(d)

	return pg, nil
}

func parseVlan(d *schema.ResourceData) (vlancfg pgVlan) {

	if vL, ok := d.GetOk("vlan"); ok {

		vlan_infos := (vL.([]interface{}))[0].(map[string]interface{})

		if v, ok := vlan_infos["type"].(string); ok && v != "" {
			vlancfg.vlanType = v
		}

		if v, ok := vlan_infos["vlan_id"].(int); ok {
			vlancfg.vlanId = int32(v)
		}

		if v, ok := vlan_infos["vlan_range"].(string); ok && v != "" {
			vlancfg.vlanRange, _ = parseVlanRange(v)
		}
	}

	return vlancfg
}

func parseVlanRange(vlanRange string) (result []types.NumericRange, errors error) {

	vlans := strings.Split(vlanRange, ",")
	var start, end int

	for _, v := range vlans {

		if v = strings.TrimSpace(v); v == "" {
			continue
		}

		if match, _ := regexp.MatchString("^(\\d+)-(\\d+)$", v); match {
			vlan := strings.Split(v, "-")
			start, _ = strconv.Atoi(vlan[0])
			end, _ = strconv.Atoi(vlan[1])

		} else if match, _ = regexp.MatchString("^\\d+$", v); match {
			start, _ = strconv.Atoi(v)
			end = start

		} else {
			return nil, fmt.Errorf("vlan range '%s' is not valid.", vlanRange)
		}

		var numRange types.NumericRange
		numRange = types.NumericRange{Start: int32(start), End: int32(end)}
		result = append(result, numRange)
	}

	return result, nil
}

func setPortSettings(vlan pgVlan) (portSettings *types.VMwareDVSPortSetting) {

	portSettings = new(types.VMwareDVSPortSetting)

	switch vlan.vlanType {
	case portgroupVlanTypeVlan:
		vlanCnf := new(types.VmwareDistributedVirtualSwitchVlanIdSpec)
		vlanCnf.VlanId = vlan.vlanId
		portSettings.Vlan = vlanCnf

	case portgroupVlanTypePVid:
		vlanCnf := new(types.VmwareDistributedVirtualSwitchPvlanSpec)
		vlanCnf.PvlanId = vlan.vlanId
		portSettings.Vlan = vlanCnf

	case portgroupVlanTypeTrunking:
		vlanCnf := new(types.VmwareDistributedVirtualSwitchTrunkVlanSpec)
		vlanCnf.VlanId = vlan.vlanRange
		portSettings.Vlan = vlanCnf

	// Nothing to do
	case portgroupVlanTypeNone:
	default:
	}

	return portSettings
}

func validateNumPorts(v interface{}, k string) (ws []string, errors []error) {
	numPorts := v.(int)

	if numPorts < portgroupNumPortsMin || numPorts > portgroupNumPortsMax {
		errors = append(errors, fmt.Errorf(
			"%s: Number of ports '%d' is out of allowed range (%d - %d).",
			k, numPorts, portgroupNumPortsMin, portgroupNumPortsMax))
	}
	return
}

func validatePortgroupType(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	found := false

	for _, t := range portgroupTypesList {
		if t == value {
			found = true
		}
	}
	if !found {
		errors = append(errors, fmt.Errorf(
			"%s: Supported values are %s", k, strings.Join(portgroupTypesList, ", ")))
	}

	return
}

func validateVlanId(v interface{}, k string) (ws []string, errors []error) {

	vlanId := v.(int)

	if vlanId < vlanIdMin || vlanId > vlanIdMax {
		errors = append(errors, fmt.Errorf(
			"%s: VLAN ID '%d' is out of range (%d - %d).",
			k, vlanId, vlanIdMin, vlanIdMax))
	}
	return
}

func validateVlanRange(v interface{}, k string) (ws []string, errors []error) {
	vlanRange := v.(string)

	parsedList, err := parseVlanRange(vlanRange)
	if err != nil {
		errors = append(errors, fmt.Errorf(
			"%s: Value %s is in incorrect format. (Example: '1-5,6,8,10-20')",
			k, vlanRange))
		return
	}

	// Additional validations
	//
	for _, v := range parsedList {

		if v.Start < vlanIdMin || v.Start > vlanIdMax {
			errors = append(errors, fmt.Errorf(
				"%s: VLAN ID %d is out of range (%d - %d)",
				k, v.Start, vlanIdMin, vlanIdMax))

		} else if v.End < vlanIdMin || v.End > vlanIdMax {
			errors = append(errors, fmt.Errorf(
				"%s: VLAN ID %d is out of range (%d - %d)",
				k, v.End, vlanIdMin, vlanIdMax))

		} else if v.End < v.Start {
			errors = append(errors, fmt.Errorf(
				"%s: %d needs to be smaller than %d",
				k, v.Start, v.End))
		}

		return
	}

	return
}

func validateVlanType(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	found := false

	for _, t := range vlanTypeList {
		if t == value {
			found = true
		}
	}
	if !found {
		errors = append(errors, fmt.Errorf(
			"%s: Supported values are %s", k, strings.Join(vlanTypeList, ", ")))
	}

	return
}

func validatePortgroupConfigs(pg *vdPortgroup) error {

	switch pg.vlanType {
	case portgroupVlanTypeVlan, portgroupVlanTypePVid:
		if pg.vlanId == 0 {
			return fmt.Errorf("vlan id is not configured for the type '%s'",
				pg.vlanType)
		}
	case portgroupVlanTypeTrunking:
		if len(pg.vlanRange) == 0 {
			return fmt.Errorf("vlan range is not configured for the type '%s'",
				pg.vlanType)
		}
	}

	return nil
}
