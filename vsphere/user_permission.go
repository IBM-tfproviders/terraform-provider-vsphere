package vsphere

import (
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

type userPermission struct {
	userName string
	roleName string

	roleId    int32
	group     bool
	propagate bool

	am *object.AuthorizationManager
	d  *schema.ResourceData
}

func NewUserPermission() *userPermission {
	p := &userPermission{
		group:     false,
		propagate: true,
	}
	return p
}

func permissionSchema() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeList,
		Optional: true,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"user_name": &schema.Schema{
					Type:     schema.TypeString,
					Required: true,
				},

				"role": &schema.Schema{
					Type:     schema.TypeString,
					Required: true,
				},
			},
		},
	}
}

func parseUserPermissionData(d *schema.ResourceData, c *govmomi.Client) *userPermission {

	p := NewUserPermission()
	p.d = d
	p.am = object.NewAuthorizationManager(c.Client)

	if permList, ok := d.GetOk("permission"); ok {
		permObj := (permList.([]interface{}))[0].(map[string]interface{})

		if v, ok := permObj["user_name"].(string); ok && v != "" {
			p.userName = v
		}

		if v, ok := permObj["role"].(string); ok && v != "" {
			p.roleName = v
		}
	}

	log.Printf("[DEBUG] User permission data %#v", p)
	return p
}

func (p *userPermission) getRoleId() error {

	roleList, err := p.am.RoleList(context.TODO())
	if err != nil {
		return err
	}

	authRole := roleList.ByName(p.roleName)
	if authRole == nil {
		return fmt.Errorf("Role '%q' not found.", p.roleName)
	}
	p.roleId = authRole.RoleId

	return nil
}

func (p *userPermission) setPermission(entity types.ManagedObjectReference) error {
	var perm types.Permission

	perm.Entity = &entity
	perm.Principal = p.userName
	perm.Group = p.group
	perm.RoleId = p.roleId
	perm.Propagate = p.propagate

	err := p.am.SetEntityPermissions(context.TODO(),
		entity, []types.Permission{perm})
	return err
}

func (p *userPermission) unsetPermission(entity types.ManagedObjectReference) error {

	err := p.am.RemoveEntityPermission(context.TODO(), entity, p.userName, p.group)
	return err
}

func (p *userPermission) setResourcePermission(entity types.ManagedObjectReference) error {

	log.Printf("[DEBUG] Setting permission while creating resource %#v.", entity)

	err := p.getRoleId()
	if err != nil {
		log.Printf("[ERROR] Could not convert role '%s' into it's ID value.", p.roleName)
		return err
	}

	log.Printf("[DEBUG] Permission being set %#v.", p)
	err = p.setPermission(entity)
	if err != nil {
		log.Printf("[ERROR] Failed to set permission to entity. Reference %#v",
			entity)
		return err
	}

	log.Printf("[DEBUG] User permission set successfully.")
	return nil
}

func (p *userPermission) updateResourcePermission(entity types.ManagedObjectReference) error {
	log.Printf("[DEBUG] Setting permission while updating resource %#v.", entity)

	old, new := p.d.GetChange("permission")
	oldPermList := old.([]interface{})
	newPermList := new.([]interface{})

	if len(oldPermList) > 0 && len(newPermList) == 0 {
		// Permission configuration removed
		// So get value of old user_name and remove permission
		//
		oldPerm := oldPermList[0].(map[string]interface{})
		if oldName, ok := oldPerm["user_name"].(string); ok && oldName != "" {
			p.userName = oldName
		}

		err := p.unsetPermission(entity)
		if err != nil {
			log.Printf("[ERROR] Could not unset permission in update operation.")
			return err
		}

	} else if len(oldPermList) == 0 && len(newPermList) > 0 {
		// Permission configuration added
		//
		err := p.setResourcePermission(entity)
		if err != nil {
			log.Printf("[ERROR] Could not set permission in update operation.")
			return err
		}

	} else {
		// Either 'user_name' and/or 'role' has been changed.
		// Preserve new name and delete old permission first.
		// Then add new permission.
		//

		newName := p.userName
		err := p.setResourcePermission(entity)
		if err != nil {
			log.Printf("[ERROR] Could not change permission in update operation.")
			return err
		}

		oldPerm := oldPermList[0].(map[string]interface{})
		oldName, ok := oldPerm["user_name"].(string)

		if ok && oldName != "" && strings.ToLower(oldName) != strings.ToLower(newName) {
			p.userName = oldName
			err = p.unsetPermission(entity)
			if err != nil {
				log.Printf("[WARN] Could not unset old permission properly.")
				return err
			}
		}
	}

	log.Printf("[DEBUG] User permission updated successfully.")
	return nil
}
