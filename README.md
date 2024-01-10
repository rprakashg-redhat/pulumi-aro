## Overview
This is a simple go program that demonstrates achieving infra structure as code with Pulumi and Azure RedHat OpenShift. Before you can try this out you will need to login to your azure subscription and request Quota increase 

## Increase Limits by VM Series

Increasing limits by VM series for Azure Red Hat OpenShift installation is necessary to ensure that your Azure Red Hat OpenShift cluster has the resources it needs to operate efficiently and reliably. 

Standard DSv3 Family vCPUs = 150  
Total Regional vCPUs = 200

Update the subscription id, tenant id in stack configuration file Pulumi.dev.yaml then run pulumi up

```
pulumi up
```

Resources that are created
* Azure Resource Group (To Create ARO Cluster and VNet)
* Azure AD Service Principal + Password
* Azure Virtual Network with 2 Subnets (Master, Worker)
* AAD Role Assignments (Network Contributor) to Cluster Service Principal and Azure RedHat OpenShift resource provider on newly created Virtual Network scope.
* Azure RedHat OpenShift Cluster

## Connecting to the cluster
Login to the cluster using the default kubeadmin account

```
export CLUSTER=arodemo
export RESOURCEGROUP=aro-rg

az aro list-credentials \
  --name $CLUSTER \
  --resource-group $RESOURCEGROUP
```

Above command will return json payload shown below that will show credentials required to connect to ARO cluster

```
{
  "kubeadminPassword": "<generated password>",
  "kubeadminUsername": "kubeadmin"
}
```

You can find the OpenShift console URL by running command below
```
 az aro show \
    --name $CLUSTER \
    --resource-group $RESOURCEGROUP \
    --query "consoleProfile.url"
```

Laumch the console in a broweser and login with the credentials retrieved earlier.
