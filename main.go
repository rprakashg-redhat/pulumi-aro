package main

import (
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/authorization"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/network"
	"github.com/pulumi/pulumi-azure-native/sdk/go/azure/resources"
	"github.com/pulumi/pulumi-azuread/sdk/v5/go/azuread"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	redhat "github.com/pulumi/pulumi-azure-native/sdk/go/azure/redhatopenshift"
)

// Values refer to structure for configuration settings that can be passed for creating an ARO cluster
type Values struct {
	Name                     string
	ClusterResourceGroupName string
	ResourceGroupName        string
	Domain                   string
	PullSecret               string
	Location                 string
	AadApp                   AadApp
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

// AadApp refers to configuration values to be used when creating AzureAD app and Service Principal
type AadApp struct {
	Name        string
	DisplayName string
	Description string
	Owners      []string
}

/*func readPullsecretAsJsonString(fileName string) (string, error) {
	var pullSecretJson string
	var content []byte
	var err error

	if content, err = os.ReadFile(fileName); err != nil {
		return "", err
	}

	//stringify json read
	pullSecretJson = string(content)

	fmt.Printf("Pull secret: %s", pullSecretJson)

	return pullSecretJson, nil
}*/

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		var v Values
		var rg *resources.ResourceGroup
		var clusterRg *resources.ResourceGroup
		var vnet *network.VirtualNetwork
		var masterSubnet *network.Subnet
		var workerSubnet *network.Subnet
		var aroCluster *redhat.OpenShiftCluster
		var aadApp *azuread.Application
		var aadSp *azuread.ServicePrincipal
		var aadSpPassword *azuread.ServicePrincipalPassword
		var pullSecret string
		var err error

		cfg := config.New(ctx, "")
		cfg.RequireObject("values", &v)

		//create the resource groups needed
		if rg, err = resources.NewResourceGroup(ctx, v.ResourceGroupName, &resources.ResourceGroupArgs{
			ResourceGroupName: pulumi.String(v.ResourceGroupName),
		}); err != nil {
			return err
		}

		if clusterRg, err = resources.NewResourceGroup(ctx, v.ClusterResourceGroupName, &resources.ResourceGroupArgs{
			ResourceGroupName: pulumi.String(v.ClusterResourceGroupName),
		}); err != nil {
			return err
		}

		//Create an AD Service Principal
		if aadApp, err = azuread.NewApplication(ctx, v.AadApp.Name, &azuread.ApplicationArgs{
			Description: pulumi.String(v.AadApp.Description),
			DisplayName: pulumi.String(v.AadApp.DisplayName),
			Owners: pulumi.ToStringArray(
				v.AadApp.Owners,
			),
		}); err != nil {
			return err
		}

		if aadSp, err = azuread.NewServicePrincipal(ctx, "arosp", &azuread.ServicePrincipalArgs{
			ApplicationId: aadApp.ApplicationId,
		}); err != nil {
			return err
		}
		//create the service principal password
		if aadSpPassword, err = azuread.NewServicePrincipalPassword(ctx, "arospPassword", &azuread.ServicePrincipalPasswordArgs{
			ServicePrincipalId: aadSp.ID(),
			EndDate:            pulumi.String("2099-01-01T00:00:00Z"),
		}); err != nil {
			return err
		}

		//create virtual network
		if vnet, err = network.NewVirtualNetwork(ctx, "virtualNetwork", &network.VirtualNetworkArgs{
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
			AddressPrefixes:    pulumi.StringArray{pulumi.String(v.Networking.Subnets[0].AddressPrefix)},
			Name:               pulumi.String(v.Networking.Subnets[0].Name),
			VirtualNetworkName: vnet.Name,
			ResourceGroupName:  rg.Name,
		}); err != nil {
			return err
		}

		if workerSubnet, err = network.NewSubnet(ctx, v.Networking.Subnets[1].Name, &network.SubnetArgs{
			AddressPrefixes:    pulumi.StringArray{pulumi.String(v.Networking.Subnets[1].AddressPrefix)},
			Name:               pulumi.String(v.Networking.Subnets[1].Name),
			VirtualNetworkName: vnet.Name,
			ResourceGroupName:  rg.Name,
		}); err != nil {
			return err
		}

		//grant network contributor permissions to service principal on vnet
		authorization.NewRoleAssignment(ctx, "roleassignment", &authorization.RoleAssignmentArgs{
			PrincipalId:        aadSp.ID(),
			PrincipalType:      pulumi.String("ServicePrincipal"),
			RoleAssignmentName: pulumi.String("349e5c2a-5cee-4bfe-9fc6-56765717a411"),
			RoleDefinitionId:   pulumi.String("/subscriptions/4f85f91d-f079-4a1e-bed7-8af80f509048/providers/Microsoft.Authorization/roleDefinitions/4d97b98b-1d4f-4787-a291-c67834d212e7"),
			Scope:              vnet.ID(),
		})
		//grant network contributor permissions to resource provider service principal on vnet
		authorization.NewRoleAssignment(ctx, "roleassignment-rp", &authorization.RoleAssignmentArgs{
			PrincipalId:        pulumi.String("fa53c24f-b862-4ff8-8259-03cc9859027c"),
			PrincipalType:      pulumi.String("ServicePrincipal"),
			RoleAssignmentName: pulumi.String("790d59fd-c011-4413-834e-e3f3cccd3f5b"),
			RoleDefinitionId:   pulumi.String("/subscriptions/4f85f91d-f079-4a1e-bed7-8af80f509048/providers/Microsoft.Authorization/roleDefinitions/4d97b98b-1d4f-4787-a291-c67834d212e7"),
			Scope:              vnet.ID(),
		})
		//also grant contributor permissions to service principal on cluster resource group
		authorization.NewRoleAssignment(ctx, "cluster-rg-contrib-assignment", &authorization.RoleAssignmentArgs{
			PrincipalId:        aadSp.ID(),
			PrincipalType:      pulumi.String("ServicePrincipal"),
			RoleAssignmentName: pulumi.String("77d3ffd6-e4d7-4555-bde7-0e2f08b08912"),
			RoleDefinitionId:   pulumi.String("/subscriptions/4f85f91d-f079-4a1e-bed7-8af80f509048/providers/Microsoft.Authorization/roleDefinitions/b24988ac-6180-42a0-ab88-20f7382dd24c"),
			Scope:              clusterRg.ID(), //pulumi.String("/subscriptions/4f85f91d-f079-4a1e-bed7-8af80f509048"),
		})

		//if pullSecret, err = readPullsecretAsJsonString("pull-secret.txt"); err != nil {
		//	return err
		//}

		//create the ARO cluster
		if aroCluster, err = redhat.NewOpenShiftCluster(ctx, v.Name, &redhat.OpenShiftClusterArgs{
			ApiserverProfile: &redhat.APIServerProfileArgs{
				Visibility: pulumi.String("Public"),
			},
			ClusterProfile: &redhat.ClusterProfileArgs{
				Domain:          pulumi.String(v.Domain),
				ResourceGroupId: clusterRg.ID(),
				PullSecret:      pulumi.String(pullSecret),
			},
			ConsoleProfile: nil,
			IngressProfiles: redhat.IngressProfileArray{
				&redhat.IngressProfileArgs{
					Name:       pulumi.String("default"),
					Visibility: pulumi.String("Public"),
				},
			},
			Location: pulumi.String(v.Location),
			MasterProfile: &redhat.MasterProfileArgs{
				SubnetId: masterSubnet.ID(),
				VmSize:   pulumi.String(v.Master.VmSize),
			},
			NetworkProfile: &redhat.NetworkProfileArgs{
				PodCidr:     pulumi.String(v.Networking.PodCidr),
				ServiceCidr: pulumi.String(v.Networking.ServiceCidr),
			},
			ResourceGroupName: rg.Name,
			ResourceName:      pulumi.String(v.Name),
			ServicePrincipalProfile: &redhat.ServicePrincipalProfileArgs{
				ClientId:     aadApp.ApplicationId,
				ClientSecret: aadSpPassword.Value,
			},
			WorkerProfiles: redhat.WorkerProfileArray{
				&redhat.WorkerProfileArgs{
					Count:      pulumi.Int(v.Worker.Count),
					DiskSizeGB: pulumi.Int(v.Worker.DiskSizeGB),
					Name:       pulumi.String(v.Worker.Name),
					SubnetId:   workerSubnet.ID(),
					VmSize:     pulumi.String(v.Worker.VmSize),
				},
			},
			Tags: pulumi.StringMap{},
		}); err != nil {
			return err
		}
		ctx.Export("kubeconfig", pulumi.All(aroCluster.Name, rg.Name, rg.ID()).ApplyT(func(args interface{}) (string, error) {
			var result *redhat.ListOpenShiftClusterAdminCredentialsResult
			var err error

			clusterName := args.([]interface{})[0].(string)
			resourceGroupNAme := args.([]interface{})[1].(string)

			if result, err = redhat.ListOpenShiftClusterAdminCredentials(ctx, &redhat.ListOpenShiftClusterAdminCredentialsArgs{
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
