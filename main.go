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

// Values refer to structure for configuration settings that can be passed for creating an ARO cluster
type Values struct {
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
}

// Networking refers to networking settings for ARO cluster
type Networking struct {
	Name            string
	AddressPrefixes string
	PodCidr         string
	ServiceCidr     string
	Subnets         []Subnet
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
	Roles       []Role
}

// Role refers to Role to be assigned for service principal
type Role struct {
	Name     string
	IdFormat string
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

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		var v Values
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
		var pullSecret string

		cfg := config.New(ctx, "")
		cfg.RequireObject("values", &v)

		subscriptionId = "4f85f91d-f079-4a1e-bed7-8af80f509048"
		fmt.Printf("Subscription ID : %s", subscriptionId)

		//create the resource group
		if rg, err = resources.NewResourceGroup(ctx, v.ResourceGroupName, &resources.ResourceGroupArgs{
			ResourceGroupName: pulumi.String(v.ResourceGroupName),
		}); err != nil {
			return err
		}

		if aadApp, err = azuread.NewApplication(ctx, uuid.New().String(), &azuread.ApplicationArgs{
			DisplayName: pulumi.String("Pulumi ARO app"),
		}); err != nil {
			return err
		}

		//Create an AAD Service Principal
		if sp, err = azuread.NewServicePrincipal(ctx, v.ServicePrincipal.Name, &azuread.ServicePrincipalArgs{
			Description: pulumi.String(v.ServicePrincipal.Description),
			ClientId:    aadApp.ClientId,
		}); err != nil {
			return err
		}

		//create the service principal password
		if spPwd, err = azuread.NewServicePrincipalPassword(ctx, fmt.Sprintf("%s-password", v.ServicePrincipal.Name), &azuread.ServicePrincipalPasswordArgs{
			ServicePrincipalId: sp.ID(),
			EndDate:            pulumi.String("2099-01-01T00:00:00Z"),
		}); err != nil {
			return err
		}

		/*
			for _, r := range v.ServicePrincipal.Roles {
				assignmentName := uuid.New()
				//grant required roles to the service principal on resource group
				authorization.NewRoleAssignment(ctx, assignmentName.String(), &authorization.RoleAssignmentArgs{
					PrincipalId:      sp.ID(),
					PrincipalType:    pulumi.String("ServicePrincipal"),
					RoleDefinitionId: pulumi.String(fmt.Sprintf(r.IdFormat, subscriptionId)),
					Scope:            rg.ID(),
				}, pulumi.DependsOn([]pulumi.Resource{rg}))
			}
		*/

		// get the service principal object id for the Azure RedHat OpenShift Resource Provider
		if aroSP, err = azuread.GetServicePrincipal(ctx, ARO_SP_NAME, pulumi.ID("fa53c24f-b862-4ff8-8259-03cc9859027c"), nil); err != nil {
			fmt.Print("Unable to look up Azure RedHat OpenShift Resource Provider Service Principal")
			return err
		}

		//create virtual network and master and worker subnets
		if vnet, err = network.NewVirtualNetwork(ctx, v.Networking.Name, &network.VirtualNetworkArgs{
			AddressSpace: &network.AddressSpaceArgs{
				AddressPrefixes: pulumi.StringArray{
					pulumi.String(v.Networking.AddressPrefixes),
				},
			},
			ResourceGroupName:  rg.Name,
			VirtualNetworkName: pulumi.String(v.Networking.Name),
		}); err != nil {
			return err
		}

		//create subnets for master and worker nodes
		if masterSubnet, err = network.NewSubnet(ctx, v.Networking.Subnets[0].Name, &network.SubnetArgs{
			AddressPrefixes:                   pulumi.StringArray{pulumi.String(v.Networking.Subnets[0].AddressPrefix)},
			SubnetName:                        pulumi.String(v.Networking.Subnets[0].Name),
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

		if workerSubnet, err = network.NewSubnet(ctx, v.Networking.Subnets[1].Name, &network.SubnetArgs{
			AddressPrefixes:                   pulumi.StringArray{pulumi.String(v.Networking.Subnets[1].AddressPrefix)},
			SubnetName:                        pulumi.String(v.Networking.Subnets[1].Name),
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

		if pullSecret, err = readPullsecretAsJsonString("pull-secret.txt"); err != nil {
			return err
		}
		clusterResourceGroupId := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionId, v.ClusterResourceGroupName)

		//create the ARO cluster
		if aroCluster, err = redhatopenshift.NewOpenShiftCluster(ctx, v.Name, &redhatopenshift.OpenShiftClusterArgs{
			ApiserverProfile: &redhatopenshift.APIServerProfileArgs{
				Visibility: pulumi.String("Public"),
			},
			ClusterProfile: &redhatopenshift.ClusterProfileArgs{
				Domain:               pulumi.String(v.Domain),
				FipsValidatedModules: pulumi.String("Enabled"),
				ResourceGroupId:      pulumi.String(clusterResourceGroupId),
				PullSecret:           pulumi.String(pullSecret),
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
			Tags: pulumi.StringMap{
				"key": pulumi.String("value"),
			},
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
