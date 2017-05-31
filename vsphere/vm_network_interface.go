package vsphere

import (
	"fmt"
	"log"
	"net"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

type networkInterface struct {
	deviceName       string
	label            string
	ipv4Address      string
	ipv4PrefixLength int
	ipv4Gateway      string
	ipv6Address      string
	ipv6PrefixLength int
	ipv6Gateway      string
	adapterType      string // TODO: Make "adapter_type" argument
	macAddress       string
}

func networkInterfaceSchema() *schema.Schema {

	return &schema.Schema{
		Type:     schema.TypeList,
		Required: true,
		ForceNew: true,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"label": &schema.Schema{
					Type:     schema.TypeString,
					Required: true,
					ForceNew: true,
				},

				"ip_address": &schema.Schema{
					Type:       schema.TypeString,
					Optional:   true,
					Computed:   true,
					Deprecated: "Please use ipv4_address",
				},

				"subnet_mask": &schema.Schema{
					Type:       schema.TypeString,
					Optional:   true,
					Computed:   true,
					Deprecated: "Please use ipv4_prefix_length",
				},

				"ipv4_address": &schema.Schema{
					Type:     schema.TypeString,
					Optional: true,
					Computed: true,
				},

				"ipv4_prefix_length": &schema.Schema{
					Type:     schema.TypeInt,
					Optional: true,
					Computed: true,
				},

				"ipv4_gateway": &schema.Schema{
					Type:     schema.TypeString,
					Optional: true,
					Computed: true,
				},

				"ipv6_address": &schema.Schema{
					Type:     schema.TypeString,
					Optional: true,
					Computed: true,
				},

				"ipv6_prefix_length": &schema.Schema{
					Type:     schema.TypeInt,
					Optional: true,
					Computed: true,
				},

				"ipv6_gateway": &schema.Schema{
					Type:     schema.TypeString,
					Optional: true,
					Computed: true,
				},

				"adapter_type": &schema.Schema{
					Type:     schema.TypeString,
					Optional: true,
					ForceNew: true,
				},

				"mac_address": &schema.Schema{
					Type:     schema.TypeString,
					Optional: true,
					Computed: true,
				},
			},
		},
	}
}

func parseNetworkInterfaceData(vL []interface{}, vm *virtualMachine) error {
	var networks []networkInterface
	for _, v := range vL {
		network := v.(map[string]interface{})
		var nic networkInterface
		nic.label = network["label"].(string)
		if v, ok := network["ip_address"].(string); ok && v != "" {
			nic.ipv4Address = v
		}
		//if v, ok := d.GetOk("gateway"); ok {
		//	nic.ipv4Gateway = v.(string)
		//}
		if v, ok := network["subnet_mask"].(string); ok && v != "" {
			ip := net.ParseIP(v).To4()
			if ip != nil {
				mask := net.IPv4Mask(ip[0], ip[1], ip[2], ip[3])
				pl, _ := mask.Size()
				nic.ipv4PrefixLength = pl
			} else {
				return fmt.Errorf("subnet_mask parameter is invalid.")
			}
		}
		if v, ok := network["ipv4_address"].(string); ok && v != "" {
			nic.ipv4Address = v
		}
		if v, ok := network["ipv4_prefix_length"].(int); ok && v != 0 {
			nic.ipv4PrefixLength = v
		}
		if v, ok := network["ipv4_gateway"].(string); ok && v != "" {
			nic.ipv4Gateway = v
		}
		if v, ok := network["ipv6_address"].(string); ok && v != "" {
			nic.ipv6Address = v
		}
		if v, ok := network["ipv6_prefix_length"].(int); ok && v != 0 {
			nic.ipv6PrefixLength = v
		}
		if v, ok := network["ipv6_gateway"].(string); ok && v != "" {
			nic.ipv6Gateway = v
		}
		if v, ok := network["mac_address"].(string); ok && v != "" {
			nic.macAddress = v
		}
		networks = append(networks, nic)
	}
	vm.networkInterfaces = networks
	return nil
}

func buildNetworkConfig(n networkInterface) (types.CustomizationAdapterMapping, error) {

	var config types.CustomizationAdapterMapping
	var ipSetting types.CustomizationIPSettings
	if n.ipv4Address == "" {
		ipSetting.Ip = &types.CustomizationDhcpIpGenerator{}
	} else {
		if n.ipv4PrefixLength == 0 {
			return config, fmt.Errorf("Error: ipv4_prefix_length argument is empty.")
		}
		m := net.CIDRMask(n.ipv4PrefixLength, 32)
		sm := net.IPv4(m[0], m[1], m[2], m[3])
		subnetMask := sm.String()
		log.Printf("[DEBUG] ipv4 gateway: %v\n", n.ipv4Gateway)
		log.Printf("[DEBUG] ipv4 address: %v\n", n.ipv4Address)
		log.Printf("[DEBUG] ipv4 prefix length: %v\n", n.ipv4PrefixLength)
		log.Printf("[DEBUG] ipv4 subnet mask: %v\n", subnetMask)
		ipSetting.Gateway = []string{
			n.ipv4Gateway,
		}
		ipSetting.Ip = &types.CustomizationFixedIp{
			IpAddress: n.ipv4Address,
		}
		ipSetting.SubnetMask = subnetMask
	}

	ipv6Spec := &types.CustomizationIPSettingsIpV6AddressSpec{}
	if n.ipv6Address == "" {
		ipv6Spec.Ip = []types.BaseCustomizationIpV6Generator{
			&types.CustomizationDhcpIpV6Generator{},
		}
	} else {
		log.Printf("[DEBUG] ipv6 gateway: %v\n", n.ipv6Gateway)
		log.Printf("[DEBUG] ipv6 address: %v\n", n.ipv6Address)
		log.Printf("[DEBUG] ipv6 prefix length: %v\n", n.ipv6PrefixLength)

		ipv6Spec.Ip = []types.BaseCustomizationIpV6Generator{
			&types.CustomizationFixedIpV6{
				IpAddress:  n.ipv6Address,
				SubnetMask: int32(n.ipv6PrefixLength),
			},
		}
		ipv6Spec.Gateway = []string{n.ipv6Gateway}
	}
	ipSetting.IpV6Spec = ipv6Spec

	// network config
	config.Adapter = ipSetting
	return config, nil
}

func addNetworkDevices(networkDevices []types.BaseVirtualDeviceConfigSpec, newVM *object.VirtualMachine) error {

	for _, dvc := range networkDevices {
		err := newVM.AddDevice(
			context.TODO(), dvc.GetVirtualDeviceConfigSpec().Device)
		if err != nil {
			return err
		}
	}
	return nil
}

func populateNetworkDeviceAndConfig(vm *virtualMachine, f *find.Finder) ([]types.BaseVirtualDeviceConfigSpec, []types.CustomizationAdapterMapping, error) {
	networkDevices := []types.BaseVirtualDeviceConfigSpec{}
	networkConfigs := []types.CustomizationAdapterMapping{}
	for _, network := range vm.networkInterfaces {
		// network device ----- TODO
		if vm.template == "" {
			network.adapterType = "e1000"
		} else {
			network.adapterType = "vmxnet3"
		}
		nd, err := buildNetworkDevice(f, network)
		if err != nil {
			return networkDevices, networkConfigs, err
		}
		log.Printf("[DEBUG] network device: %+v", nd.Device)
		networkDevices = append(networkDevices, nd)

		if vm.template != "" { // TODO
			config, err := buildNetworkConfig(network)
			if err != nil {
				return networkDevices, networkConfigs, err
			}
			networkConfigs = append(networkConfigs, config)
		}
	}
	return networkDevices, networkConfigs, nil
}

// buildNetworkDevice builds VirtualDeviceConfigSpec for Network Device.
func buildNetworkDevice(f *find.Finder, n networkInterface) (*types.VirtualDeviceConfigSpec, error) {
	log.Printf("[DEBUG] network interface ======= %+v", n)
	network, err := f.Network(context.TODO(), "*"+n.label)
	if err != nil {
		return nil, err
	}

	backing, err := network.EthernetCardBackingInfo(context.TODO())
	if err != nil {
		return nil, err
	}

	var address_type string
	if n.macAddress == "" {
		address_type = string(types.VirtualEthernetCardMacTypeGenerated)
	} else {
		address_type = string(types.VirtualEthernetCardMacTypeManual)
	}

	if n.adapterType == "vmxnet3" {
		return &types.VirtualDeviceConfigSpec{
			Operation: types.VirtualDeviceConfigSpecOperationAdd,
			Device: &types.VirtualVmxnet3{
				VirtualVmxnet: types.VirtualVmxnet{
					VirtualEthernetCard: types.VirtualEthernetCard{
						VirtualDevice: types.VirtualDevice{
							Key:     -1,
							Backing: backing,
						},
						AddressType: address_type,
						MacAddress:  n.macAddress,
					},
				},
			},
		}, nil
	} else if n.adapterType == "e1000" {
		return &types.VirtualDeviceConfigSpec{
			Operation: types.VirtualDeviceConfigSpecOperationAdd,
			Device: &types.VirtualE1000{
				VirtualEthernetCard: types.VirtualEthernetCard{
					VirtualDevice: types.VirtualDevice{
						Key:     -1,
						Backing: backing,
					},
					AddressType: address_type,
					MacAddress:  n.macAddress,
				},
			},
		}, nil
	} else {
		return nil, fmt.Errorf("Invalid network n.adapter type.")
	}
}
