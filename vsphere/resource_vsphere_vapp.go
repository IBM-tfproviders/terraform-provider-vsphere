package vsphere

import (
	"fmt"
	"log"
	"path"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

const (
	entityInputVm      = "vm"
	entityInputVapp    = "vapp"
	vAppEntityTypeVm   = "VirtualMachine"
	vAppEntityTypeVApp = "VirtualApp"

	vAppStartOrderMin     = 0
	vAppStartOrderMax     = 2147483648
	vAppStartOrderDefault = 0
)

var entityTypeList = []string{
	string(entityInputVm),
	string(entityInputVapp),
}

var diskFormatTypeList = []string{
	string(types.VAppCloneSpecProvisioningTypeSameAsSource),
	string(types.VAppCloneSpecProvisioningTypeThin),
	string(types.VAppCloneSpecProvisioningTypeThick),
}

var stopActionList = []string{
	string(types.VAppAutoStartActionNone),
	string(types.VAppAutoStartActionPowerOff),
	string(types.VAppAutoStartActionGuestShutdown),
	string(types.VAppAutoStartActionSuspend),
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
	// Can't we store in a seprate variable types.VAppEntityConfigInfo so that it can be used directly
	types.VAppEntityConfigInfo
	name             string
	entityType       string
	entityFolderPath string
	entityRPPath     string
	entityMoid       string
	entityRef        types.ManagedObjectReference
	folder           string
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

	// TODO we need to remove
	memory types.ResourceAllocationInfo
	cpu    types.ResourceAllocationInfo

	vAppToClone  templateVApp
	vAppEntities []vAppEntity

	c               *govmomi.Client
	d               *schema.ResourceData
	createdVApp     *object.VirtualApp
	dcFolders       *object.DatacenterFolders
	folderObj       *object.Folder
	finder          *find.Finder
	resourcePoolObj *object.ResourcePool
	datastoreRef    types.ManagedObjectReference
}

func resourceVSphereVApp() *schema.Resource {
	return &schema.Resource{
		Create: resourceVSphereVAppCreate,
		Read:   resourceVSphereVAppRead,
		Update: resourceVSphereVAppUpdate,
		Delete: resourceVSphereVAppDelete,

		SchemaVersion: 1,

		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Default:  "Created by Terraform",
			},
			"uuid": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"datacenter": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"datastore": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"cluster": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				//ForceNew: true,
			},
			"resource_pool": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				//ForceNew: true,
			},
			"folder": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				//ForceNew: true,
			},
			"parent_vapp": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				//ForceNew: true,
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
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validateEntityType,
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
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validateStartAction,
						},
						"stop_action": &schema.Schema{
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validateStopAction,
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
						"folder_path": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},
						"moid": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},
						"resourcepool_path": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
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
							ForceNew: true,
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

	log.Printf("[DEBUG] resourceVSphereVAppCreate :: ResourceData d: %#v", d)

	// Construct vAPP Object with required Attributes
	vapp, _ := NewVApp(d, client)

	err := vapp.populateOptionalVAppAttributes(d)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while reading Optional Input attributes: %s", err)
		return err
	}

	// Populate Datacenter and finder
	dc, err := getDatacenter(vapp.c, vapp.datacenter)
	if err != nil {
		return err
	}
	vapp.finder = find.NewFinder(vapp.c.Client, true)
	vapp.finder = vapp.finder.SetDatacenter(dc)
	vapp.dcFolders, err = dc.Folders(context.TODO())
	if err != nil {
		return err
	}

	err = vapp.populateVAppTemplate(d)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while reading VApp Template attributes: %s", err)
		return err
	}

	err = vapp.populateVAppEntities(d)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while reading VApp Entity attributes: %s", err)
		return err
	}

	err = vapp.populateVAppResourceAllocationInfo(d)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while reading VApp Resource Allocation attributes: %s", err)
		return err
	}

	log.Printf("[DEBUG] resourceVSphereVAppCreate :: vapp : %#v", vapp)

	err = vapp.calculateLocation()
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while finding resource location : %s", err)
		return err
	}

	err = vapp.create()
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while creating VApp : %s", err)
		return err
	}

	if len(vapp.vAppEntities) > 0 {
		err := addEntities(vapp.c, vapp.finder, vapp.createdVApp, vapp.vAppEntities)
		if err != nil {
			log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while adding Entities into VApp: %s", err)
			return err
		}
	}
	log.Printf("[DEBUG] resourceVSphereVAppCreate :: vapp.vAppEntities : %#v", vapp.vAppEntities)

	//err = vapp.updateEntities()
	err = vapp.updateEntities(vapp.vAppEntities)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while updating VApp to modify Entities : %s", err)
		return err
	}

	err = powerOnVApp(vapp.createdVApp)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppCreate :: Error while Powering On VApp: %s", err)
		return err
	}

	vAppPath := getVAppPath(d)
	d.SetId(vAppPath)

	//Back Populate folder and resourcepool path
	entities := make([]map[string]interface{}, 0)
	for i, vappEntity := range vapp.vAppEntities {
		if vL, ok := d.GetOk("entity"); ok {
			if entitySet, ok := vL.(*schema.Set); ok {
				for _, value := range entitySet.List() {
					entity := value.(map[string]interface{})
					if entity["name"] == vappEntity.name && 
                       getEntityType(entity["type"].(string)) == vappEntity.entityType {
						entity["folder_path"] = vapp.vAppEntities[i].entityFolderPath
						entity["resourcepool_path"] = vapp.vAppEntities[i].entityRPPath
						entity["moid"] = vapp.vAppEntities[i].entityMoid
						entities = append(entities, entity)
						log.Printf("[DEBUG] entity : %#v", entity)
					}
				}
			}
		}
	}
	err = d.Set("entity", entities)
	if err != nil {
		return fmt.Errorf("Invalid entity to set: %#v", entities)
	}

	// Read the Vapp properties
	var mvapp mo.VirtualApp
	collector := property.DefaultCollector(vapp.c.Client)
	if err := collector.RetrieveOne(context.TODO(), vapp.createdVApp.Reference(), []string{"vAppConfig"}, &mvapp); err != nil {
		return err
	}
	log.Printf("[DEBUG] mvapp: %#v", mvapp.VAppConfig.EntityConfig)
	//var vmRef *types.ManagedObjectReference
	for _, entities := range mvapp.VAppConfig.EntityConfig {
		vmRef := entities.Key
		log.Printf("[DEBUG] mvapp.vmRef: %#v", vmRef)
		log.Printf("[DEBUG] mvapp.Key: %#v", entities.Key)
	}

	return resourceVSphereVAppRead(d, meta)
}

func resourceVSphereVAppRead(d *schema.ResourceData, meta interface{}) error {

	log.Printf("[DEBUG] resourceVSphereVAppRead:: d : %#v", d)

	client := meta.(*govmomi.Client)
	dc, err := getDatacenter(client, d.Get("datacenter").(string))
	if err != nil {
		return err
	}

	finder := find.NewFinder(client.Client, true)
	finder = finder.SetDatacenter(dc)

	vapp, err := finder.VirtualApp(context.TODO(), d.Id())
	if err != nil {
		d.SetId("")
		return nil
	}

	var mvapp mo.VirtualApp
	collector := property.DefaultCollector(client.Client)
	if err := collector.RetrieveOne(context.TODO(), vapp.Reference(), []string{"vAppConfig"}, &mvapp); err != nil {
		return err
	}

	d.Set("uuid", mvapp.VAppConfig.InstanceUuid)

	return nil

}

func resourceVSphereVAppUpdate(d *schema.ResourceData, meta interface{}) error {

	log.Printf("[DEBUG] resourceVSphereVAppUpdate :: Update Operation.")
	client := meta.(*govmomi.Client)

	log.Printf("[DEBUG] resourceVSphereVAppUpdate:: ResourceData d: %#v", d)

	// Construct vAPP Object with required Attributes
	vapp, _ := NewVApp(d, client)

	dc, err := getDatacenter(client, d.Get("datacenter").(string))
	if err != nil {
		return err
	}
	vapp.finder = find.NewFinder(vapp.c.Client, true)
	vapp.finder = vapp.finder.SetDatacenter(dc)
	vapp.createdVApp, err = getCreatedVApp(d, vapp.finder)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppUpdate :: Error while finding VApp: %s", err)
		return err
	}

	if d.HasChange("entity") {
		oldEntities, newEntities := d.GetChange("entity")
		oldEntitySet := oldEntities.(*schema.Set)
		newEntitySet := newEntities.(*schema.Set)

		addedEntities := newEntitySet.Difference(oldEntitySet)
		removedEntities := oldEntitySet.Difference(newEntitySet)

		log.Printf("[DEBUG] addedEntities : %#v\n", addedEntities)
		log.Printf("[DEBUG] removedEntities : %#v\n", removedEntities)

		//Finding the Modifed Entities
		modifiedEntities := make([]map[string]interface{}, 0)
		for _, value := range addedEntities.List() {
			addedEntity := value.(map[string]interface{})
			for _, value := range removedEntities.List() {
				removedEntity := value.(map[string]interface{})
				if addedEntity["name"] == removedEntity["name"] && addedEntity["type"] == removedEntity["type"] {
					log.Printf("[DEBUG] Mofifying the enity %#v", addedEntity)
					addedEntities.Remove(addedEntity)
					removedEntities.Remove(removedEntity)
					addedEntity["moid"] = removedEntity["moid"]
					addedEntity["folder_path"] = removedEntity["folder_path"]
					addedEntity["resourcepool_path"] = removedEntity["resourcepool_path"]
					modifiedEntities = append(modifiedEntities, addedEntity)
					log.Printf("[DEBUG] Modifying the enity %#v after", addedEntity)
					break
				}
			}
		}

		log.Printf("[DEBUG] addedEntities : %#v\n", addedEntities)
		log.Printf("[DEBUG] removedEntities : %#v\n", removedEntities)
		log.Printf("[DEBUG] modifiedEntities : %#v\n", modifiedEntities)

		//Populate Added Entities
		vappAddedEntities := []vAppEntity{}
		for _, value := range addedEntities.List() {
			entity := value.(map[string]interface{})
			newEntity := vAppEntity{}

			newEntity.name = entity["name"].(string)
			newEntity.entityType = getEntityType(entity["type"].(string))

			if v, ok := entity["folder"].(string); ok && v != "" {
				newEntity.folder = v
			}
			if v, ok := entity["start_order"].(int); ok {
				newEntity.StartOrder = int32(v)
			}
			if v, ok := entity["start_delay"].(int); ok && v != 0 {
				newEntity.StartDelay = int32(v)
			}
			if v, ok := entity["stop_delay"].(int); ok && v != 0 {
				newEntity.StopDelay = int32(v)
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
			vappAddedEntities = append(vappAddedEntities, newEntity)

		}

		log.Printf("[DEBUG] addedEntities : %#v\n", vappAddedEntities)
		if addedEntities.Len() > 0 {
			err := addEntities(vapp.c, vapp.finder, vapp.createdVApp, vappAddedEntities)
			if err != nil {
				return err
			}
			log.Printf("[DEBUG] addedEntities after addition : %#v\n", vappAddedEntities)
		}

		if removedEntities.Len() > 0 {
			err = removeEntities(vapp.c, removedEntities)
			if err != nil {
				return err
			}
		}

		//Populate Modified Entities
		vappModifiedEntities := []vAppEntity{}
		for _, entity := range modifiedEntities {
			//entity := value.(map[string]interface{})
			newEntity := vAppEntity{}

			newEntity.name = entity["name"].(string)
			newEntity.entityType = getEntityType(entity["type"].(string))

			if v, ok := entity["folder"].(string); ok && v != "" {
				newEntity.folder = v
			}
			if v, ok := entity["start_order"].(int); ok {
				newEntity.StartOrder = int32(v)
			}
			if v, ok := entity["start_delay"].(int); ok && v != 0 {
				newEntity.StartDelay = int32(v)
			}
			if v, ok := entity["stop_delay"].(int); ok && v != 0 {
				newEntity.StopDelay = int32(v)
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
			if v, ok := entity["moid"].(string); ok {
				newEntity.entityMoid = v
			}
			if v, ok := entity["folder_path"].(string); ok && v != "" {
				newEntity.entityFolderPath = v
			}
			if v, ok := entity["resourcepool_path"].(string); ok && v != "" {
				newEntity.entityRPPath = v
			}
			vappModifiedEntities = append(vappModifiedEntities, newEntity)

		}

		log.Printf("[DEBUG] vappModifiedEntities : %#v\n", vappModifiedEntities)

		for _, v := range vappAddedEntities {
			vappModifiedEntities = append(vappModifiedEntities, v)
		}

		if len(vappModifiedEntities) > 0 {
			err = vapp.updateEntities(vappModifiedEntities)
			if err != nil {
				return err
			}
		}

		log.Printf("[DEBUG] vappModifiedEntities after append : %#v\n", vappModifiedEntities)
		//Back Populate folder and resourcepool path
		entities := make([]map[string]interface{}, 0)
		if vL, ok := d.GetOk("entity"); ok {
			if entitySet, ok := vL.(*schema.Set); ok {
				for _, value := range entitySet.List() {
					entity := value.(map[string]interface{})
					log.Printf("[DEBUG] entity : %#v\n", entity)
					added := 0
					for _, vappEntity := range vappModifiedEntities {
						if entity["name"] == vappEntity.name && getEntityType(entity["type"].(string)) == vappEntity.entityType { // Need to add folder also TODO
							entity["folder_path"] = vappEntity.entityFolderPath
							entity["resourcepool_path"] = vappEntity.entityRPPath
							entity["moid"] = vappEntity.entityMoid
							entities = append(entities, entity)
							added = 1
							break
							log.Printf("[DEBUG] added disk updated")
						}
					}
					if added == 0 {
						log.Printf("[DEBUG] old disk updated")
						entities = append(entities, entity)
					}
				}
			}
		}
		err = d.Set("entity", entities)
		if err != nil {
			return fmt.Errorf("Invalid entity to set: %#v", entities)
		}
	}
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
	if err != nil {
		return err
	}
	createdVApp, err := getCreatedVApp(d, finder)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppDelete :: Error while finding VApp: %s", err)
		return err
	}

	err = powerOffVApp(createdVApp)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppDelete :: Error while powering Off VApp: %s", err)
		return err
	}

	if vL, ok := d.GetOk("entity"); ok {
		if entitySet, ok := vL.(*schema.Set); ok {
			if entitySet.Len() > 0 {
				err = removeEntities(client, entitySet)
				if err != nil {
					log.Printf("[ERROR] resourceVSphereVAppDelete :: Error while removing entities from VApp: %s", err)
					return err
				}
			}
		}
	}

	err = destroyVApp(createdVApp)
	if err != nil {
		log.Printf("[ERROR] resourceVSphereVAppDelete :: Error while deleting VApp: %s", err)
		return err
	}

	d.SetId("")

	return nil
}

func NewVApp(d *schema.ResourceData, c *govmomi.Client) (*vApp, error) {

	// Construct vAPP Object with required Attributes
	vapp := &vApp{
		d:    d,
		c:    c,
		name: d.Get("name").(string),
	}

	return vapp, nil
}

func getCreatedVApp(d *schema.ResourceData, f *find.Finder) (*object.VirtualApp, error) {

	vAppPath := getVAppPath(d)

	log.Printf("[DEBUG] getCreatedVApp:: finding the Created VApp: %s", vAppPath)

	vapp, err := f.VirtualApp(context.TODO(), vAppPath)

	log.Printf("[DEBUG] getCreatedVApp:: Created VApp: %s", vapp)

	if err != nil {
		log.Printf("[ERROR] Couldn't able to find the Created VApp: %s", vAppPath)
		return nil, err
	}

	return vapp, nil

}

func getVAppPath(d *schema.ResourceData) string {

	vAppName := d.Get("name").(string)
	vAppPath := vAppName

	if v, ok := d.GetOk("parent_vapp"); ok && v != "" {
		vAppPath = vAppPathString(v.(string), vAppName)

	} else if v, ok := d.GetOk("folder"); ok && v != "" {

		vAppPath = vAppPathString(v.(string), vAppName)

	}
	return vAppPath

}

func powerOnVApp(vapp *object.VirtualApp) error {

	task, err := vapp.PowerOn(context.TODO())
	if err != nil {
		return err
	}
	err = task.Wait(context.TODO())
	if err != nil {
		return err
	}
	return nil

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

func vAppPathString(parentFolder string, name string) string {
	var path string
	if len(parentFolder) > 0 {
		path += parentFolder + "/"
	}
	return path + name
}

func (vapp *vApp) getVmref() (*types.ManagedObjectReference, error) {
	sourceVApp, err := vapp.finder.VirtualApp(context.TODO(), vapp.vAppToClone.name)
	if err != nil {
		log.Printf("[ERROR] Coundn't able to find the vapp: %s, to be cloned ", vapp.vAppToClone.name)
		return nil, err
	}
	log.Printf("[DEBUG] sourceVApp: %#v", sourceVApp.ResourcePool)

	// Read the Vapp properties
	var mvapp mo.VirtualApp
	collector := property.DefaultCollector(vapp.c.Client)
	if err := collector.RetrieveOne(context.TODO(), sourceVApp.Reference(), []string{"vAppConfig"}, &mvapp); err != nil {
		return nil, err
	}
	log.Printf("[DEBUG] mvapp: %#v", mvapp.VAppConfig.EntityConfig)
	var vmRef *types.ManagedObjectReference
	for _, entities := range mvapp.VAppConfig.EntityConfig {
		vmRef = entities.Key
	}
	log.Printf("[DEBUG] vmRef: %#v", vmRef)
	return vmRef, nil
}

func (vapp *vApp) calculateDatastore() error {
	var datastore *object.Datastore
	var err error
	if vapp.datastore == "" {
		datastore, err = vapp.finder.DefaultDatastore(context.TODO())
		if err != nil {
			return err
		}
	} else {
		datastore, err = vapp.finder.Datastore(context.TODO(), vapp.datastore)
		if err != nil {
			d, err := getDatastoreObject(vapp.c, vapp.dcFolders, vapp.datastore)
			if err != nil {
				return err
			}
			if d.Type == "StoragePod" {
				sp := object.StoragePod{
					Folder: object.NewFolder(vapp.c.Client, d),
				}
				spr := sp.Reference()
				rpr := vapp.resourcePoolObj.Reference()
				vmfr := vapp.folderObj.Reference()
				// Getting a vm reference from Source VApp object
				vmRef, err := vapp.getVmref()
				if err != nil {
					log.Printf("[ERROR] Coundn't able to find a vm in vmRef")
					return err
				}
				sps := types.StoragePlacementSpec{
					Type: "clone",
					Vm:   vmRef,
					PodSelectionSpec: types.StorageDrsPodSelectionSpec{
						StoragePod: &spr,
					},
					CloneSpec: &types.VirtualMachineCloneSpec{
						Location: types.VirtualMachineRelocateSpec{
							Pool: &rpr,
						},
					},
					CloneName: "dummy",
					Folder:    &vmfr,
				}
				datastore, err = findDatastore(vapp.c, sps)
				if err != nil {
					return err
				}
			} else {
				datastore = object.NewDatastore(vapp.c.Client, d)
			}
		}
	}
	vapp.datastoreRef = datastore.Reference()
	log.Printf("[DEBUG] datastore: %#v", datastore)
	return nil
}

func (vapp *vApp) calculateResourcePool() error {
	var err error
	var resourcePool *object.ResourcePool
	var parentVApp *object.VirtualApp
	if vapp.parentVApp != "" {
		parentVApp, err = vapp.finder.VirtualApp(context.TODO(), vapp.parentVApp)
		if err != nil {
			return err
		}
		resourcePool = parentVApp.ResourcePool
	} else if vapp.resourcePool == "" {
		if vapp.cluster == "" {
			resourcePool, err = vapp.finder.DefaultResourcePool(context.TODO())
			if err != nil {
				return err
			}
		} else {
			resourcePool, err = vapp.finder.ResourcePool(context.TODO(), "*"+vapp.cluster+"/Resources")
			if err != nil {
				return err
			}
		}
	} else {
		resourcePool, err = vapp.finder.ResourcePool(context.TODO(), vapp.resourcePool)
		if err != nil {
			return err
		}
	}
	vapp.resourcePoolObj = resourcePool
	log.Printf("[DEBUG] resource pool: %#v", resourcePool)
	return nil
}

func (vapp *vApp) calculateLocation() error {

	var err error
	// Finding or Calculating the resourcePool
	err = vapp.calculateResourcePool()
	if err != nil {
		return err
	}

	// Finding or Calculating the Folder
	folder := vapp.dcFolders.VmFolder
	if len(vapp.folder) > 0 {
		folder, err = findFolder(vapp.c, vapp.datacenter, vapp.folder)
		if err != nil {
			return err
		}
	}
	log.Printf("[DEBUG] folder: %#v", folder)
	vapp.folderObj = folder
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

	var err error
	// Finding or Calculating the Datastore
	err = vapp.calculateDatastore()
	if err != nil {
		return err
	}

	// Getting the Source VApp object
	sourceVApp, err := vapp.finder.VirtualApp(context.TODO(), vapp.vAppToClone.name)
	if err != nil {
		log.Printf("[ERROR] Coundn't able to find the vapp: %s, to be cloned ", vapp.vAppToClone.name)
		return err
	}

	// Creating VAppCloneSpecNetworkMappingPair
	networkMappingPairs := []types.VAppCloneSpecNetworkMappingPair{}
	for _, networkMapping := range vapp.vAppToClone.networkMappings {
		networkMappingPair := types.VAppCloneSpecNetworkMappingPair{}

		networkObj, err := vapp.finder.Network(context.TODO(), networkMapping.srcNetLabel)
		if err != nil {
			log.Printf("[ERROR] Coundn't able to find the network: %s", networkMapping.srcNetLabel)
			return err
		}
		networkMappingPair.Source = networkObj.Reference()

		networkObj, err = vapp.finder.Network(context.TODO(), networkMapping.destNetLabel)
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
	vapp.createdVApp, err = getCreatedVApp(vapp.d, vapp.finder)
	if err != nil {
		return err
	}

	/*	// Powering On the Created VirtualApp
		task, err = vapp.createdVApp.PowerOn(context.TODO())
		if err != nil {
			log.Printf("[ERROR] Coundn't able to find the Created VApp: %s", vapp.name)
			return err
		}
		err = task.Wait(context.TODO())
		if err != nil {
			return err
		}*/
	return nil
}

func createDefaultResourceAllocation() types.BaseResourceAllocationInfo {
	var info types.BaseResourceAllocationInfo
	info = new(types.ResourceAllocationInfo)
	ra := info.GetResourceAllocationInfo()
	ra.Reservation = 1
	ra.Limit = -1
	ra.Shares = new(types.SharesInfo)
	ra.Shares.Level = types.SharesLevelNormal
	ra.ExpandableReservation = types.NewBool(true)
	return info
}

func (vapp *vApp) createVApp() error {
	log.Printf("[DEBUG] Creating vapp via create api")

	resSpec := new(types.ResourceConfigSpec)
	resSpec.MemoryAllocation = createDefaultResourceAllocation()
	resSpec.CpuAllocation = createDefaultResourceAllocation()

	configSpec := types.VAppConfigSpec{}
	folder := vapp.folderObj
	var err error
	log.Printf("[DEBUG] resSpec : %#v", resSpec)
	log.Printf("[DEBUG] CpuAllocation : %#v", resSpec.CpuAllocation)
	log.Printf("[DEBUG] MemoryAllocation : %#v", resSpec.MemoryAllocation)
	log.Printf("[DEBUG] configSpec : %#v", configSpec)
	log.Printf("[DEBUG] folder : %#v", folder)
	vapp.createdVApp, err = vapp.resourcePoolObj.CreateVApp(context.TODO(), vapp.name, *resSpec, configSpec, folder)
	log.Printf("[DEBUG] createdVApp : %#v", vapp.createdVApp)
	return err
}

func (vapp *vApp) updateEntities(vAppEntities []vAppEntity) error {
	configSpec := types.VAppConfigSpec{}
	vappEntitiesConfigInfo := []types.VAppEntityConfigInfo{}
	for _, vappEntity := range vAppEntities {
		log.Printf("[DEBUG] vappEntity : %#v", vappEntity)
		vappEntityConfigInfo := types.VAppEntityConfigInfo{}
		vappEntityConfigInfo.StartOrder = vappEntity.StartOrder
		vappEntityConfigInfo.StartDelay = vappEntity.StartDelay
		vappEntityConfigInfo.WaitingForGuest = vappEntity.WaitingForGuest
		vappEntityConfigInfo.StartAction = vappEntity.StartAction
		vappEntityConfigInfo.StopDelay = vappEntity.StopDelay
		vappEntityConfigInfo.StopAction = vappEntity.StopAction

		// Prepare the EnityList
		entityRef := types.ManagedObjectReference{}
		entityRef.Type = vappEntity.entityType
		entityRef.Value = vappEntity.entityMoid
		vappEntityConfigInfo.Key = &entityRef
		vappEntitiesConfigInfo = append(vappEntitiesConfigInfo, vappEntityConfigInfo)
	}
	configSpec.EntityConfig = vappEntitiesConfigInfo
	log.Printf("[DEBUG] configSpec : %#v", configSpec)
	return vapp.createdVApp.UpdateConfig(context.TODO(), configSpec)
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

	log.Printf("[DEBUG] populateVAppEntities called")
	if vL, ok := d.GetOk("entity"); ok {
		if entitySet, ok := vL.(*schema.Set); ok {
			entities := []vAppEntity{}
			for _, value := range entitySet.List() {
				entity := value.(map[string]interface{})
				newEntity := vAppEntity{}

				newEntity.name = entity["name"].(string)
				newEntity.entityType = getEntityType(entity["type"].(string))

				if v, ok := entity["folder"].(string); ok && v != "" {
					newEntity.folder = v
				}
				if v, ok := entity["start_order"].(int); ok {
					newEntity.StartOrder = int32(v)
				}
				if v, ok := entity["start_delay"].(int); ok && v != 0 {
					newEntity.StartDelay = int32(v)
				}
				if v, ok := entity["stop_delay"].(int); ok && v != 0 {
					newEntity.StopDelay = int32(v)
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

func findFolder(c *govmomi.Client, datacenter string, folderName string) (*object.Folder, error) {
	var folder *object.Folder
	si := object.NewSearchIndex(c.Client)
	folderRef, err := si.FindByInventoryPath(
		context.TODO(), fmt.Sprintf("%v/vm/%v", datacenter, folderName))
	if err != nil {
		return nil, fmt.Errorf("Error reading folder %s: %s", folderName, err)
	} else if folderRef == nil {
		return nil, fmt.Errorf("Cannot find folder %s", folderName)
	} else {
		folder = folderRef.(*object.Folder)
	}
	return folder, nil
}

func getEntityRef(finder *find.Finder, entityType string, entityName string) (types.ManagedObjectReference, string, error) {
	log.Printf("[DEBUG] entity absolute name: %#v", entityName)
	var entityRef types.ManagedObjectReference
	var entityFolderPath string
	if entityType == vAppEntityTypeVm {
		entity, err := finder.VirtualMachine(context.TODO(), entityName)
		if err != nil {
			return entityRef, entityFolderPath, err
		}
		entityRef = entity.Reference()
		entityFolderPath = path.Dir(entity.InventoryPath)
		log.Printf("[DEBUG] entityFolderPath : %#v", entityFolderPath)
	} else if entityType == vAppEntityTypeVApp {
		entity, err := finder.VirtualApp(context.TODO(), entityName)
		if err != nil {
			return entityRef, entityFolderPath, err
		}
		entityRef = entity.Reference()
		entityFolderPath = path.Dir(entity.InventoryPath)
		log.Printf("[DEBUG] entityFolderPath : %#v", entityFolderPath)
	} else {
		return entityRef, entityFolderPath, fmt.Errorf("vappEntity Type should be either vm or vapp")
	}
	log.Printf("[DEBUG] entityRef : %#v", entityRef)
	return entityRef, entityFolderPath, nil
}

func validateEntityType(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if value != entityInputVm && value != entityInputVapp {
		errors = append(errors, fmt.Errorf(
			"%s: Supported values are %s", k, strings.Join(entityTypeList, ", ")))
	}
	return
}

func validateStartAction(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if value != string(types.VAppAutoStartActionNone) && value != string(types.VAppAutoStartActionPowerOn) {
		errors = append(errors, fmt.Errorf(
			"only '%s', and '%s' are supported values for 'start_action'", string(types.VAppAutoStartActionNone), string(types.VAppAutoStartActionPowerOn)))
	}
	return
}

func validateStopAction(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	found := false

	for _, t := range stopActionList {
		if t == value {
			found = true
		}
	}
	if !found {
		errors = append(errors, fmt.Errorf(
			"%s: Supported values are %s", k, strings.Join(stopActionList, ", ")))
	}

	return
}

func addEntities(c *govmomi.Client, finder *find.Finder, vapp *object.VirtualApp, vAppEntities []vAppEntity) error {
	//Get the Entities Object Ref
	var entityList []types.ManagedObjectReference
	for i, vappEntity := range vAppEntities {
		entityFullName := vAppPathString(vappEntity.folder, vappEntity.name)
		entityRef, entityPath, err := getEntityRef(finder, vappEntity.entityType, entityFullName)
		if err != nil {
			return err
		}
		vAppEntities[i].entityRef = entityRef
		vAppEntities[i].entityFolderPath = entityPath
		vAppEntities[i].entityMoid = entityRef.Value
		entityList = append(entityList, entityRef)
		if vappEntity.entityType == vAppEntityTypeVm {
			var mo mo.VirtualMachine
			collector := property.DefaultCollector(c.Client)
			if err := collector.RetrieveOne(context.TODO(), entityRef, []string{"resourcePool"}, &mo); err != nil {
				return err
			}
			log.Printf("[DEBUG] mo.ResourcePool : %#v", mo.ResourcePool)
			Element, _ := finder.Element(context.TODO(), *mo.ResourcePool)
			vAppEntities[i].entityRPPath = Element.Path
		} else if vappEntity.entityType == vAppEntityTypeVApp {
			var mo mo.VirtualApp
			collector := property.DefaultCollector(c.Client)
			if err := collector.RetrieveOne(context.TODO(), entityRef, []string{"parent"}, &mo); err != nil {
				return err
			}
			log.Printf("[DEBUG] mo.Parent : %#v", mo.Parent)
			Element, _ := finder.Element(context.TODO(), *mo.Parent)
			vAppEntities[i].entityRPPath = Element.Path
		} else {
			return fmt.Errorf("vappEntity Type should be either vm or vapp")
		}
	}
	log.Printf("[DEBUG] addEntities :: vAppEntities : %#v", vAppEntities)

	// Creating the req for MoveIntoResourcePool
	req := types.MoveIntoResourcePool{
		This: vapp.Reference(),
		List: entityList,
	}
	log.Printf("[DEBUG] addEntities : req %#v", req)
	_, err := methods.MoveIntoResourcePool(context.TODO(), c, &req)
	if err != nil {
		return err
	}

	return nil
}

func removeEntities(c *govmomi.Client, entitySet *schema.Set) error {
	for _, value := range entitySet.List() {
		entity := value.(map[string]interface{})
		entityType := getEntityType(entity["type"].(string))
		entityMoid := entity["moid"].(string)
		entityFolderPath := entity["folder_path"].(string)
		entityRPPath := entity["resourcepool_path"].(string)

		// Prepare the EnityList
		entityRef := types.ManagedObjectReference{}
		entityRef.Type = entityType
		entityRef.Value = entityMoid

		var entityList []types.ManagedObjectReference
		entityList = append(entityList, entityRef)

		// Find Resource pool Reference
		si := object.NewSearchIndex(c.Client)
		resourcePoolObjRef, err := si.FindByInventoryPath(
			context.TODO(), entityRPPath)
		if err != nil {
			return fmt.Errorf("Error reading resource pool %s: %s", entityRPPath, err)
		} else if resourcePoolObjRef == nil {
			return fmt.Errorf("Cannot find resource pool %s", entityRPPath)
		}
		resourcePoolRef := resourcePoolObjRef.Reference()

		// Moving the entity to the Previous ResourcePool
		req := types.MoveIntoResourcePool{
			This: resourcePoolRef,
			List: entityList,
		}
		_, err = methods.MoveIntoResourcePool(context.TODO(), c, &req)
		if err != nil {
			return err
		}

		// Find Folder Reference
		si = object.NewSearchIndex(c.Client)
		folderObjRef, err := si.FindByInventoryPath(
			context.TODO(), entityFolderPath)
		if err != nil {
			return fmt.Errorf("Error reading folder %s: %s", entityFolderPath, err)
		} else if folderObjRef == nil {
			return fmt.Errorf("Cannot find folder %s", entityFolderPath)
		}
		folderRef := folderObjRef.Reference()

		// Moving the entity to the Previous Folder
		reqf := types.MoveIntoFolder_Task{
			This: folderRef,
			List: entityList,
		}
		_, err = methods.MoveIntoFolder_Task(context.TODO(), c, &reqf)
		if err != nil {
			return err
		}

	}
	return nil
}

func getEntityType(eType string) string {
	if eType == entityInputVm {
		return vAppEntityTypeVm
	} else if eType == entityInputVapp {
		return vAppEntityTypeVApp
	} else {
		return "UNKNOWN"
	}

}
