package vsphere

import (
	"fmt"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

const (
	vAppEntityTypeVm   = "vm"
	vAppEntityTypeVApp = "vapp"

	vAppStartOrderMin     = 0
	vAppStartOrderMax     = 2147483648
	vAppStartOrderDefault = 0
)

var diskFormatTypeList = []string{
	string(types.VAppCloneSpecProvisioningTypeSameAsSource),
	string(types.VAppCloneSpecProvisioningTypeThin),
	string(types.VAppCloneSpecProvisioningTypeThick),
}

type vAppNetworkMapping struct {
	srcNetLabel  string
	destNetLabel string
}

type templateVApp struct {
	name            string
	diskFormat      types.VAppCloneSpecProvisioningType
	networkMappings []vAppNetworkMapping
}

type vAppEntity struct {
	types.VAppEntityConfigInfo
	name       string
	entityType string
	folder     string
}

type vApp struct {
	name         string
	description  string
	datacenter   string
	datastore    string
	cluster      string
	resourcePool string
	folder       string
	parentVApp   string

	memory types.ResourceAllocationInfo
	cpu    types.ResourceAllocationInfo

	vAppToClone  templateVApp
	vAppEntities []vAppEntity

	c               *govmomi.Client
	d               *schema.ResourceData
	folderObj       *object.Folder
	resourcePoolObj *object.ResourcePool
	datastoreRef    types.ManagedObjectReference
}

func resourceVSphereVApp() *schema.Resource {
	return &schema.Resource{
		Create: resourceVSphereVAppCreate,
		Read:   resourceVSphereVAppRead,
		Update: resourceVSphereVAppUpdate,
		Delete: resourceVSphereVAppDelete,

		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"uuid": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"datacenter": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"datastore": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"cluster": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"resource_pool": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"folder": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"parent_vapp": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"entity": &schema.Schema{
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"folder": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"type": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"key": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},
						"start_order": &schema.Schema{
							Type:     schema.TypeInt,
							Optional: true,
							Default:  vAppStartOrderDefault,
						},
						"start_delay": &schema.Schema{
							Type:     schema.TypeInt,
							Optional: true,
						},
						"start_action": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"stop_action": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
						},
						"stop_delay": &schema.Schema{
							Type:     schema.TypeInt,
							Optional: true,
						},
						"waiting_for_guest": &schema.Schema{
							Type:     schema.TypeBool,
							Optional: true,
						},
						"destroy_with_parent": &schema.Schema{
							Type:     schema.TypeBool,
							Optional: true,
						},
					},
				},
			},
			"template_vapp": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"disk_provisioning": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
							Default:  types.VAppCloneSpecProvisioningTypeSameAsSource,
						},
						"network_mapping": &schema.Schema{
							Type:     schema.TypeSet,
							Optional: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"source_network_label": &schema.Schema{
										Type:     schema.TypeString,
										Required: true,
									},
									"destination_network_label": &schema.Schema{
										Type:     schema.TypeString,
										Required: true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func resourceVSphereVAppCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*govmomi.Client)

	log.Printf("[DEBUG] resourceVSphereVAppCreate :: client : %#v", client)
	log.Printf("[DEBUG] resourceVSphereVAppCreate :: ResourceData d: %#v", d)

	// Construct vAPP Object with required Attributes
	vapp := vApp{
		d:    d,
		c:    client,
		name: d.Get("name").(string),
	}

	err := vapp.populateOptionalVAppAttributes(d)
	if err != nil {
		log.Printf("[ERROR] %s", err)
		return err
	}

	err = vapp.populateVAppTemplate(d)
	if err != nil {
		log.Printf("[ERROR] %s", err)
		return err
	}

	err = vapp.populateVAppEntities(d)
	if err != nil {
		log.Printf("[ERROR] %s", err)
		return err
	}

	err = vapp.populateVAppResourceAllocationInfo(d)
	if err != nil {
		log.Printf("[ERROR] %s", err)
		return err
	}

	log.Printf("[DEBUG] resourceVSphereVAppCreate :: vapp : %#v", vapp)

	// VApp Creation

	err = vapp.calculateLocation()
	if err != nil {
		log.Printf("[ERROR] %s", err)
		return err
	}

	err = vapp.create()
	if err != nil {
		log.Printf("[ERROR] %s", err)
		return err
	}

	err = vapp.linkEntities()
	if err != nil {
		log.Printf("[ERROR] %s", err)
		return err
	}
	d.SetId(vapp.name)
	return resourceVSphereVAppRead(d, meta)
}

func resourceVSphereVAppRead(d *schema.ResourceData, meta interface{}) error {

	log.Printf("[DEBUG] resourceVSphereVAppRead:: d : %#v", d)

	d.Set("name", d.Get("name"))

	log.Printf("[DEBUG] resourceVSphereVAppRead:: d : %#v", d)

	return nil
}
func resourceVSphereVAppUpdate(d *schema.ResourceData, meta interface{}) error {
	return nil
}

func resourceVSphereVAppDelete(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*govmomi.Client)

	log.Printf("[DEBUG] resourceVSphereVAppDelete:: ResourceData d: %#v", d)

	dc, err := getDatacenter(client, d.Get("datacenter").(string))
	if err != nil {
		return err
	}

	finder := find.NewFinder(client.Client, true)
	finder = finder.SetDatacenter(dc)

	vapp, err := getCreatedVApp(d, finder)
	if err != nil {
		return err
	}

	err = powerOffVApp(vapp)
	if err != nil {
		return err
	}

	err = unLinkEntities(d, vapp)
	if err != nil {
		return err
	}

	err = destroyVApp(vapp)
	if err != nil {
		return err
	}

	d.SetId("")

	return nil

}

func getCreatedVApp(d *schema.ResourceData, f *find.Finder) (*object.VirtualApp, error) {

	vAppName := d.Get("name").(string)
	vAppPath := vAppName

	if v, ok := d.GetOk("parent_vapp"); ok && v != "" {
		vAppPath = vAppPathString(v.(string), vAppName)

	} else if v, ok := d.GetOk("folder"); ok && v != "" {

		vAppPath = vAppPathString(v.(string), vAppName)

	}

	log.Printf("[DEBUG] getCreatedVApp:: finding the Created VApp: %s", vAppPath)

	vapp, err := f.VirtualApp(context.TODO(), vAppPath)

	log.Printf("[DEBUG] getCreatedVApp:: Created VApp: %s", vapp)

	if err != nil {
		log.Printf("[ERROR] Couldn't able to find the Created VApp: %s", vAppPath)
		return nil, err
	}

	return vapp, nil

}

func powerOffVApp(vapp *object.VirtualApp) error {

	task, err := vapp.PowerOff(context.TODO(), false)
	if err != nil {
		return err
	}
	err = task.Wait(context.TODO())
	if err != nil {
		err = task.Wait(context.TODO())
		if err != nil {
			// ignore if the vapp is already powered off
			if f, ok := err.(types.HasFault); ok {
				switch f.Fault().(type) {
				case *types.InvalidPowerState:
					return nil
				}
			}
			return err
		}
	}
	return nil

}

func destroyVApp(vapp *object.VirtualApp) error {

	task, err := vapp.Destroy(context.TODO())
	if err != nil {
		return err
	}
	err = task.Wait(context.TODO())
	if err != nil {
		return err
	}
	return nil

}

func unLinkEntities(d *schema.ResourceData, vapp *object.VirtualApp) error {

	log.Printf("[DEBUG] unLinkEntities:: TBD...")
	return nil
}

func vAppPathString(parentFolder string, name string) string {
	var path string
	if len(parentFolder) > 0 {
		path += parentFolder + "/"
	}
	return path + name
}

func (vapp *vApp) calculateLocation() error {

	dc, err := getDatacenter(vapp.c, vapp.datacenter)
	if err != nil {
		return err
	}
	finder := find.NewFinder(vapp.c.Client, true)
	finder = finder.SetDatacenter(dc)

	// Finding or Calculating the resourcePool
	var resourcePool *object.ResourcePool
	if vapp.parentVApp != "" {
		parentVApp, err := finder.VirtualApp(context.TODO(), vapp.parentVApp)
		if err != nil {
			return err
		}
		resourcePool = parentVApp.ResourcePool
		log.Printf("[DEBUG] resource pool: %#v", resourcePool)
	} else if vapp.resourcePool == "" {
		if vapp.cluster == "" {
			resourcePool, err = finder.DefaultResourcePool(context.TODO())
			if err != nil {
				return err
			}
		} else {
			resourcePool, err = finder.ResourcePool(context.TODO(), "*"+vapp.cluster+"/Resources")
			if err != nil {
				return err
			}
		}
	} else {
		resourcePool, err = finder.ResourcePool(context.TODO(), vapp.resourcePool)
		if err != nil {
			return err
		}
	}
	vapp.resourcePoolObj = resourcePool
	log.Printf("[DEBUG] resource pool: %#v", resourcePool)

	// Finding or Calculating the Folder
	dcFolders, err := dc.Folders(context.TODO())
	if err != nil {
		return err
	}

	folder := dcFolders.VmFolder
	if len(vapp.folder) > 0 {
		si := object.NewSearchIndex(vapp.c.Client)
		folderRef, err := si.FindByInventoryPath(
			context.TODO(), fmt.Sprintf("%v/vm/%v", vapp.datacenter, vapp.folder))
		if err != nil {
			return fmt.Errorf("Error reading folder %s: %s", vapp.folder, err)
		} else if folderRef == nil {
			return fmt.Errorf("Cannot find folder %s", vapp.folder)
		} else {
			folder = folderRef.(*object.Folder)
		}
	}
	log.Printf("[DEBUG] folder: %#v", folder)
	vapp.folderObj = folder

	// Finding or Calculating the Datastore
	var datastore *object.Datastore
	if vapp.datastore == "" {
		datastore, err = finder.DefaultDatastore(context.TODO())
		if err != nil {
			return err
		}
	} else {
		datastore, err = finder.Datastore(context.TODO(), vapp.datastore)
		if err != nil {
			d, err := getDatastoreObject(vapp.c, dcFolders, vapp.datastore)
			if err != nil {
				return err
			}
			datastore = object.NewDatastore(vapp.c.Client, d)
			log.Printf("[DEBUG] datastore: %#v", datastore)
			if d.Type == "StoragePod" {
				log.Printf("[ERROR] The given datastore is a StoragePod")
				return fmt.Errorf("StoragePod is not supported in case of VApp")
			} else {
				datastore = object.NewDatastore(vapp.c.Client, d)
			}
		}
	}
	vapp.datastoreRef = datastore.Reference()
	log.Printf("[DEBUG] datastore: %#v", datastore)
	return nil
}

func (vapp *vApp) create() error {
	if _, ok := vapp.d.GetOk("template_vapp"); ok {
		log.Printf("[DEBUG] Creating vapp via clone api")
		return vapp.cloneVApp()
	} else {
		log.Printf("[DEBUG] Creating vapp via create api")
		return vapp.createVApp()
	}
}

func (vapp *vApp) cloneVApp() error {

	dc, err := getDatacenter(vapp.c, vapp.datacenter)
	if err != nil {
		return err
	}
	finder := find.NewFinder(vapp.c.Client, true)
	finder = finder.SetDatacenter(dc)

	// Getting the Source VApp object
	sourceVApp, err := finder.VirtualApp(context.TODO(), vapp.vAppToClone.name)
	if err != nil {
		log.Printf("[ERROR] Coundn't able to find the vapp: %s, to be cloned ", vapp.vAppToClone.name)
		return err
	}

	// Creating VAppCloneSpecNetworkMappingPair
	networkMappingPairs := []types.VAppCloneSpecNetworkMappingPair{}
	for _, networkMapping := range vapp.vAppToClone.networkMappings {
		networkMappingPair := types.VAppCloneSpecNetworkMappingPair{}

		networkObj, err := finder.Network(context.TODO(), networkMapping.srcNetLabel)
		if err != nil {
			log.Printf("[ERROR] Coundn't able to find the network: %s", networkMapping.srcNetLabel)
			return err
		}
		networkMappingPair.Source = networkObj.Reference()

		networkObj, err = finder.Network(context.TODO(), networkMapping.destNetLabel)
		if err != nil {
			log.Printf("[ERROR] Coundn't able to find the network: %s", networkMapping.destNetLabel)
			return err
		}
		networkMappingPair.Destination = networkObj.Reference()

		networkMappingPairs = append(networkMappingPairs, networkMappingPair)
	}

	// Creating the VAppCloneSpec
	folder := vapp.folderObj.Reference()
	vappCloneSpec := types.VAppCloneSpec{
		Location:       vapp.datastoreRef,
		Provisioning:   string(vapp.vAppToClone.diskFormat),
		NetworkMapping: networkMappingPairs,
	}

	// Adding the folder only if parent vapp is not specified
	if vapp.parentVApp == "" {
		vappCloneSpec.VmFolder = &folder
	}

	// Creating the req for CloneVApp_Task
	req := types.CloneVApp_Task{
		This:   sourceVApp.Reference(),
		Name:   vapp.name,
		Target: vapp.resourcePoolObj.Reference(),
		Spec:   vappCloneSpec,
	}

	// Cloning the VApp TODO: vapp.c is the client I am passing
	res, err := methods.CloneVApp_Task(context.TODO(), vapp.c, &req)
	if err != nil {
		return err
	}
	task := object.NewTask(vapp.c.Client, res.Returnval)
	if err != nil {
		return err
	}
	err = task.Wait(context.TODO())
	if err != nil {
		return err
	}

	// Getting the  Created VirtualApp Object
	//createdVApp, err = finder.VirtualApp(context.TODO(), vapp.name)
	createdVApp, err := getCreatedVApp(vapp.d, finder)

	if err != nil {
		return err
	}

	// Powering On the Created VirtualApp
	task, err = createdVApp.PowerOn(context.TODO())
	if err != nil {
		log.Printf("[ERROR] Coundn't able to find the Created VApp: %s", vapp.name)
		return err
	}
	err = task.Wait(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func (vapp *vApp) createVApp() error {
	log.Printf("[DEBUG] Creating vapp via create api is still not supported")
	return nil
}

func (vapp *vApp) linkEntities() error {
	return nil
}

func (vapp *vApp) populateOptionalVAppAttributes(d *schema.ResourceData) error {

	if v, ok := d.GetOk("description"); ok && v != "" {
		vapp.description = v.(string)
	}

	if v, ok := d.GetOk("datacenter"); ok && v != "" {
		vapp.datacenter = v.(string)
	}

	if v, ok := d.GetOk("datastore"); ok && v != "" {
		vapp.datastore = v.(string)
	}

	if v, ok := d.GetOk("cluster"); ok && v != "" {
		vapp.cluster = v.(string)
	}

	if v, ok := d.GetOk("resource_pool"); ok && v != "" {
		vapp.resourcePool = v.(string)
	}

	if v, ok := d.GetOk("folder"); ok && v != "" {
		vapp.folder = v.(string)
	}

	if v, ok := d.GetOk("parent_vapp"); ok && v != "" {
		vapp.parentVApp = v.(string)
	}

	return nil
}

func (vapp *vApp) populateVAppEntities(d *schema.ResourceData) error {

	if vL, ok := d.GetOk("entity"); ok {
		if entitySet, ok := vL.(*schema.Set); ok {
			entities := []vAppEntity{}
			for _, value := range entitySet.List() {
				entity := value.(map[string]interface{})
				newEntity := vAppEntity{}

				newEntity.name = entity["name"].(string)
				newEntity.entityType = entity["type"].(string)

				if v, ok := entity["folder"].(string); ok && v != "" {
					newEntity.folder = v
				}
				if v, ok := entity["start_order"].(int32); ok {
					newEntity.StartOrder = v
				}
				if v, ok := entity["start_delay"].(int32); ok && v != 0 {
					newEntity.StartDelay = v
				}
				if v, ok := entity["stop_delay"].(int32); ok && v != 0 {
					newEntity.StopDelay = v
				}
				if v, ok := entity["wait_for_guest"].(bool); ok {
					newEntity.WaitingForGuest = &v
				}
				if v, ok := entity["start_action"].(string); ok && v != "" {
					newEntity.StartAction = v
				}
				if v, ok := entity["stop_action"].(string); ok && v != "" {
					newEntity.StopAction = v
				}
				entities = append(entities, newEntity)

			}
			vapp.vAppEntities = entities
		}
	}
	return nil
}

func (vapp *vApp) populateVAppTemplate(d *schema.ResourceData) error {

	if vL, ok := d.GetOk("template_vapp"); ok {

		template := (vL.([]interface{}))[0].(map[string]interface{})

		vAppTemplate := templateVApp{
			name: template["name"].(string),
		}

		if v, ok := template["disk_provisioning"].(string); ok && v != "" {
			vAppTemplate.diskFormat = types.VAppCloneSpecProvisioningType(v)
		}

		if netMaps, ok := template["network_mapping"]; ok && netMaps != nil {

			if netMapSet, ok := netMaps.(*schema.Set); ok {
				netMappings := []vAppNetworkMapping{}
				for _, value := range netMapSet.List() {
					netMap := value.(map[string]interface{})
					newNetMap := vAppNetworkMapping{}

					newNetMap.srcNetLabel = netMap["source_network_label"].(string)
					newNetMap.destNetLabel = netMap["destination_network_label"].(string)

					netMappings = append(netMappings, newNetMap)
				}
				vAppTemplate.networkMappings = netMappings
			}
		}

		vapp.vAppToClone = vAppTemplate
	}

	return nil
}

func (vapp *vApp) populateVAppResourceAllocationInfo(d *schema.ResourceData) error {

	log.Printf("[DEBUG] TBD.....")
	return nil
}
