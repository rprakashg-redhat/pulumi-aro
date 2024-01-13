package main

import (
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/pulumi/pulumi-azure-native-sdk/authorization/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/network/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/redhatopenshift/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi-azuread/sdk/v5/go/azuread"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

const ARO_SP_NAME = "Azure Red Hat OpenShift RP"

type AzureNative struct {
	Location       string
	SubscriptionId string
	TenantId       string
}

// Values refer to structure for configuration settings that can be passed for creating an ARO cluster
type ConfigData struct {
	ClusterResourceGroupName string
	ResourceGroupName        string
	Name                     string
	Domain                   string
	Location                 string
	ServicePrincipal         ServicePrincipal
	PullSecret               string
	Networking               Networking
	Master                   MasterProfile
	Worker                   WorkerProfile
	Tags                     map[string]string
}

// Networking refers to networking settings for ARO cluster
type Networking struct {
	Name          string
	AddressPrefix string
	PodCidr       string
	ServiceCidr   string
	MasterSubnet  Subnet
	WorkerSubnet  Subnet
}

// Subnet refers to custom subnets for master and worker nodes to be used
type Subnet struct {
	Name          string
	AddressPrefix string
}

// ControlPlaneProfile refers to configuration of Master nodes in ARO Cluster
type MasterProfile struct {
	Count  int
	VmSize string
}

// WorkerProfile refers to configuration for worker nodes in ARO cluster
type WorkerProfile struct {
	Count      int
	VmSize     string
	DiskSizeGB int
	Name       string
}

// ServicePrincipal refers to configuration for service principal resource will be created in AAD
type ServicePrincipal struct {
	Name        string
	Description string
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		var configData ConfigData
		var err error
		var subscriptionId string
		var rg *resources.ResourceGroup
		var vnet *network.VirtualNetwork
		var masterSubnet *network.Subnet
		var workerSubnet *network.Subnet
		var aroCluster *redhatopenshift.OpenShiftCluster
		var aadApp *azuread.Application
		var sp *azuread.ServicePrincipal
		var aroSP *azuread.ServicePrincipal
		var spPwd *azuread.ServicePrincipalPassword
		var azureNativeConfig AzureNative

		configData = readConfig(ctx)

		subscriptionId = "4f85f91d-f079-4a1e-bed7-8af80f509048"
		fmt.Printf("Subscription ID : %s", subscriptionId)

		//create the resource group
		if rg, err = resources.NewResourceGroup(ctx, configData.ResourceGroupName, &resources.ResourceGroupArgs{
			ResourceGroupName: pulumi.String(configData.ResourceGroupName),
		}); err != nil {
			return err
		}

		if aadApp, err = azuread.NewApplication(ctx, uuid.New().String(), &azuread.ApplicationArgs{
			DisplayName: pulumi.String("Pulumi ARO app"),
		}); err != nil {
			return err
		}

		//Create an AAD Service Principal
		if sp, err = azuread.NewServicePrincipal(ctx, configData.ServicePrincipal.Name, &azuread.ServicePrincipalArgs{
			Description: pulumi.String(configData.ServicePrincipal.Description),
			ClientId:    aadApp.ClientId,
		}); err != nil {
			return err
		}

		//create the service principal password
		if spPwd, err = azuread.NewServicePrincipalPassword(ctx, fmt.Sprintf("%s-password", configData.ServicePrincipal.Name), &azuread.ServicePrincipalPasswordArgs{
			ServicePrincipalId: sp.ID(),
			EndDate:            pulumi.String("2099-01-01T00:00:00Z"),
		}); err != nil {
			return err
		}

		// get the service principal object id for the Azure RedHat OpenShift Resource Provider
		if aroSP, err = azuread.GetServicePrincipal(ctx, ARO_SP_NAME, pulumi.ID("fa53c24f-b862-4ff8-8259-03cc9859027c"), nil); err != nil {
			fmt.Print("Unable to look up Azure RedHat OpenShift Resource Provider Service Principal")
			return err
		}

		//create virtual network and master and worker subnets
		if vnet, err = network.NewVirtualNetwork(ctx, configData.Networking.Name, &network.VirtualNetworkArgs{
			AddressSpace: &network.AddressSpaceArgs{
				AddressPrefixes: pulumi.StringArray{
					pulumi.String(configData.Networking.AddressPrefix),
				},
			},
			ResourceGroupName:  rg.Name,
			VirtualNetworkName: pulumi.String(configData.Networking.Name),
		}); err != nil {
			return err
		}

		//create subnets for master and worker nodes
		if masterSubnet, err = network.NewSubnet(ctx, configData.Networking.MasterSubnet.Name, &network.SubnetArgs{
			AddressPrefixes:                   pulumi.StringArray{pulumi.String(configData.Networking.MasterSubnet.AddressPrefix)},
			SubnetName:                        pulumi.String(configData.Networking.MasterSubnet.Name),
			VirtualNetworkName:                vnet.Name,
			ResourceGroupName:                 rg.Name,
			PrivateLinkServiceNetworkPolicies: pulumi.String("Disabled"),
			ServiceEndpoints: network.ServiceEndpointPropertiesFormatArray{
				network.ServiceEndpointPropertiesFormatArgs{
					Service: pulumi.String("Microsoft.ContainerRegistry"),
				},
			},
		}); err != nil {
			return err
		}

		if workerSubnet, err = network.NewSubnet(ctx, configData.Networking.WorkerSubnet.Name, &network.SubnetArgs{
			AddressPrefixes:                   pulumi.StringArray{pulumi.String(configData.Networking.WorkerSubnet.AddressPrefix)},
			SubnetName:                        pulumi.String(configData.Networking.WorkerSubnet.Name),
			VirtualNetworkName:                vnet.Name,
			ResourceGroupName:                 rg.Name,
			PrivateLinkServiceNetworkPolicies: pulumi.String("Disabled"),
			ServiceEndpoints: network.ServiceEndpointPropertiesFormatArray{
				network.ServiceEndpointPropertiesFormatArgs{
					Service: pulumi.String("Microsoft.ContainerRegistry"),
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{masterSubnet})); err != nil {
			return err
		}

		assignmentName := uuid.New()
		//grant  network contributor permissions to service principal on vnet
		authorization.NewRoleAssignment(ctx, assignmentName.String(), &authorization.RoleAssignmentArgs{
			PrincipalId:      sp.ID(),
			PrincipalType:    pulumi.String("ServicePrincipal"),
			RoleDefinitionId: pulumi.String(fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/4d97b98b-1d4f-4787-a291-c67834d212e7", subscriptionId)),
			Scope:            vnet.ID(),
		})
		//grant network contributor permissions to ARO provider service principal on vnet
		assignmentName = uuid.New()
		authorization.NewRoleAssignment(ctx, assignmentName.String(), &authorization.RoleAssignmentArgs{
			PrincipalId:      aroSP.ID(),
			PrincipalType:    pulumi.String("ServicePrincipal"),
			RoleDefinitionId: pulumi.String(fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/4d97b98b-1d4f-4787-a291-c67834d212e7", subscriptionId)),
			Scope:            vnet.ID(),
		})

		if configData.PullSecret != "" {
			//read from local
			if pullSecret, err := readPullsecretAsJsonString("pull-secret.txt"); err != nil {
				return err
			} else {
				configData.PullSecret = pullSecret
			}
		}

		//read subscription id
		cfg := config.New(ctx, "")
		cfg.RequireObject("azure-native", &azureNativeConfig)
		clusterResourceGroupId := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", azureNativeConfig.SubscriptionId, configData.ClusterResourceGroupName)

		//create the ARO cluster
		if aroCluster, err = redhatopenshift.NewOpenShiftCluster(ctx, configData.Name, &redhatopenshift.OpenShiftClusterArgs{
			ApiserverProfile: &redhatopenshift.APIServerProfileArgs{
				Visibility: pulumi.String("Public"),
			},
			ClusterProfile: &redhatopenshift.ClusterProfileArgs{
				Domain:               pulumi.String(configData.Domain),
				FipsValidatedModules: pulumi.String("Enabled"),
				ResourceGroupId:      pulumi.String(clusterResourceGroupId),
				PullSecret:           pulumi.String(configData.PullSecret),
			},
			ConsoleProfile: nil,
			IngressProfiles: redhatopenshift.IngressProfileArray{
				&redhatopenshift.IngressProfileArgs{
					Name:       pulumi.String("default"),
					Visibility: pulumi.String("Public"),
				},
			},
			Location: pulumi.String(v.Location),
			MasterProfile: &redhatopenshift.MasterProfileArgs{
				EncryptionAtHost: pulumi.String("Disabled"),
				SubnetId:         masterSubnet.ID(),
				VmSize:           pulumi.String(v.Master.VmSize),
			},
			NetworkProfile: &redhatopenshift.NetworkProfileArgs{
				PodCidr:     pulumi.String(v.Networking.PodCidr),
				ServiceCidr: pulumi.String(v.Networking.ServiceCidr),
			},
			ResourceGroupName: rg.Name,
			ResourceName:      pulumi.String(v.Name),
			ServicePrincipalProfile: &redhatopenshift.ServicePrincipalProfileArgs{
				ClientId:     sp.ClientId,
				ClientSecret: spPwd.Value,
			},
			WorkerProfiles: redhatopenshift.WorkerProfileArray{
				&redhatopenshift.WorkerProfileArgs{
					Count:            pulumi.Int(v.Worker.Count),
					DiskSizeGB:       pulumi.Int(v.Worker.DiskSizeGB),
					Name:             pulumi.String(v.Worker.Name),
					SubnetId:         workerSubnet.ID(),
					VmSize:           pulumi.String(v.Worker.VmSize),
					EncryptionAtHost: pulumi.String("Disabled"),
				},
			},
			Tags: pulumi.ToStringMap(v.Tags),
		}, pulumi.DependsOn([]pulumi.Resource{rg, vnet, masterSubnet, workerSubnet})); err != nil {
			return err
		}

		ctx.Export("kubeconfig", pulumi.All(aroCluster.Name, rg.Name, rg.ID()).ApplyT(func(args interface{}) (string, error) {
			var result *redhatopenshift.ListOpenShiftClusterAdminCredentialsResult
			var err error

			clusterName := args.([]interface{})[0].(string)
			resourceGroupNAme := args.([]interface{})[1].(string)

			if result, err = redhatopenshift.ListOpenShiftClusterAdminCredentials(ctx, &redhatopenshift.ListOpenShiftClusterAdminCredentialsArgs{
				ResourceGroupName: resourceGroupNAme,
				ResourceName:      clusterName,
			}); err != nil {
				return "", err
			}

			return *result.Kubeconfig, nil
		}))
		return nil
	})
}

func readPullsecretAsJsonString(fileName string) (string, error) {
	var pullSecretJson string
	var content []byte
	var err error

	if content, err = os.ReadFile(fileName); err != nil {
		return "", err
	}

	//stringify json read
	pullSecretJson = string(content)

	return pullSecretJson, nil
}

func readConfig(ctx *pulumi.Context) ConfigData {
	var configData = ConfigData{}
	var tags map[string]string

	cfg := config.New(ctx, "")

	if clusterResourceGroupName, err := cfg.Try("clusterResourceGroupName"); err != nil {
		configData.ClusterResourceGroupName = "aro-infra-rg"
	} else {
		configData.ClusterResourceGroupName = clusterResourceGroupName
	}

	if resourceGroupName, err := cfg.Try("resourceGroupName"); err != nil {
		configData.ResourceGroupName = "aro-rg"
	} else {
		configData.ResourceGroupName = resourceGroupName
	}

	if name, err := cfg.Try("name"); err != nil {
		configData.Name = "arodemo"
	} else {
		configData.Name = name
	}

	if domain, err := cfg.Try("domain"); err != nil {
		configData.Domain = "demos"
	} else {
		configData.Domain = domain
	}

	if location, err := cfg.Try("location"); err != nil {
		configData.Location = "EastUS"
	} else {
		configData.Location = location
	}

	configData.ServicePrincipal = ServicePrincipal{}
	if servicePrincipalName, err := cfg.Try("servicePrincipalName"); err != nil {
		configData.ServicePrincipal.Name = "arodemo-sp"
	} else {
		configData.ServicePrincipal.Name = servicePrincipalName
	}
	if desc, err := cfg.Try("servicePrincipalDescription"); err != nil {
		configData.ServicePrincipal.Description = "aro demo service principal"
	} else {
		configData.ServicePrincipal.Description = desc
	}

	configData.Master = MasterProfile{}
	configData.Master.Count = 3
	if vmSize, err := cfg.Try("masterVmSize"); err != nil {
		configData.Master.VmSize = "Standard_D8s_v3"
	} else {
		configData.Master.VmSize = vmSize

	}

	configData.Worker = WorkerProfile{}
	if workerName, err := cfg.Try("workerName"); err != nil {
		configData.Worker.Name = "worker"
	} else {
		configData.Worker.Name = workerName
	}
	if workerVmSize, err := cfg.Try("workerVmSize"); err != nil {
		configData.Worker.VmSize = "Standard_D4s_v3"
	} else {
		configData.Worker.VmSize = workerVmSize
	}
	if workerDiskSize, err := cfg.TryInt("workerDiskSize"); err != nil {
		configData.Worker.DiskSizeGB = 128
	} else {
		configData.Worker.DiskSizeGB = workerDiskSize
	}
	if workerNodeCount, err := cfg.TryInt("workerNodeCount"); err != nil {
		configData.Worker.Count = 3
	} else {
		configData.Worker.Count = workerNodeCount
	}

	configData.Networking = Networking{}
	if vnetName, err := cfg.Try("vnetName"); err != nil {
		configData.Networking.Name = "arodemo-vnet"
	} else {
		configData.Networking.Name = vnetName
	}
	if vnetAddressPrefix, err := cfg.Try("vnetAddressPrefix"); err != nil {
		configData.Networking.AddressPrefix = "10.0.0.0/22"
	} else {
		configData.Networking.AddressPrefix = vnetAddressPrefix
	}
	if podCidr, err := cfg.Try("podCidr"); err != nil {
		configData.Networking.PodCidr = "10.128.0.0/14"
	} else {
		configData.Networking.PodCidr = podCidr
	}
	if serviceCidr, err := cfg.Try("serviceCidr"); err != nil {
		configData.Networking.ServiceCidr = "172.30.0.0/16"
	} else {
		configData.Networking.ServiceCidr = serviceCidr
	}
	configData.Networking.MasterSubnet = Subnet{}
	if masterSubnetName, err := cfg.Try("masterSubnetName"); err != nil {
		configData.Networking.MasterSubnet.Name = "master"
	} else {
		configData.Networking.MasterSubnet.Name = masterSubnetName
	}
	if masterSubnetAddressPrefix, err := cfg.Try("masterSubnetAddressPrefix"); err != nil {
		configData.Networking.MasterSubnet.AddressPrefix = "10.0.0.0/23"
	} else {
		configData.Networking.MasterSubnet.AddressPrefix = masterSubnetAddressPrefix
	}
	configData.Networking.WorkerSubnet = Subnet{}
	if workerSubnetName, err := cfg.Try("workerSubnetName"); err != nil {
		configData.Networking.WorkerSubnet.Name = "worker"
	} else {
		configData.Networking.WorkerSubnet.Name = workerSubnetName
	}
	if workerSubnetAddressPrefix, err := cfg.Try("workerSubnetAddressPrefix"); err != nil {
		configData.Networking.WorkerSubnet.AddressPrefix = "10.0.2.0/23"
	} else {
		configData.Networking.WorkerSubnet.AddressPrefix = workerSubnetAddressPrefix
	}

	if pullSecret, err := cfg.Try("pullSecret"); err != nil {
		configData.PullSecret = ""
	} else {
		configData.PullSecret = pullSecret
	}
	cfg.TryObject("tags", &tags)
	configData.Tags = tags

	return configData
}
