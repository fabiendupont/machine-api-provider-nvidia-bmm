package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NvidiaBMMMachineProviderSpec defines the desired state for OpenShift Machine API
type NvidiaBMMMachineProviderSpec struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// SiteID is the NVIDIA BMM Site UUID
	// +required
	SiteID string `json:"siteId"`

	// TenantID is the NVIDIA BMM tenant ID
	// +required
	TenantID string `json:"tenantId"`

	// InstanceTypeID specifies the NVIDIA BMM instance type UUID
	// Mutually exclusive with MachineID
	// +optional
	InstanceTypeID string `json:"instanceTypeId,omitempty"`

	// MachineID specifies a specific machine UUID for targeted provisioning
	// Mutually exclusive with InstanceTypeID
	// +optional
	MachineID string `json:"machineId,omitempty"`

	// AllowUnhealthyMachine allows provisioning on an unhealthy machine
	// +optional
	AllowUnhealthyMachine bool `json:"allowUnhealthyMachine,omitempty"`

	// VpcID is the VPC UUID
	// +required
	VpcID string `json:"vpcId"`

	// SubnetID is the primary subnet UUID
	// +required
	SubnetID string `json:"subnetId"`

	// AdditionalSubnetIDs for multi-NIC configurations
	// +optional
	AdditionalSubnetIDs []AdditionalSubnet `json:"additionalSubnetIds,omitempty"`

	// UserData contains the cloud-init user data
	// +optional
	UserData string `json:"userData,omitempty"`

	// SSHKeyGroupIDs contains SSH key group IDs
	// +optional
	SSHKeyGroupIDs []string `json:"sshKeyGroupIds,omitempty"`

	// Labels to apply to the NVIDIA BMM instance
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// CredentialsSecret references a secret containing NVIDIA BMM API credentials
	// The secret must contain: endpoint, orgName, token
	// +required
	CredentialsSecret CredentialsSecretReference `json:"credentialsSecret"`
}

// AdditionalSubnet defines an additional network interface
type AdditionalSubnet struct {
	// SubnetID is the subnet UUID for this interface
	// +required
	SubnetID string `json:"subnetId"`

	// IsPhysical indicates if this is a physical interface
	// +optional
	IsPhysical bool `json:"isPhysical,omitempty"`
}

// CredentialsSecretReference contains information to locate the secret
type CredentialsSecretReference struct {
	// Name of the secret
	// +required
	Name string `json:"name"`

	// Namespace of the secret
	// +required
	Namespace string `json:"namespace"`
}

// NvidiaBMMMachineProviderStatus defines the observed state for OpenShift Machine API
type NvidiaBMMMachineProviderStatus struct {
	metav1.TypeMeta `json:",inline"`

	// InstanceID is the NVIDIA BMM instance ID
	// +optional
	InstanceID *string `json:"instanceId,omitempty"`

	// MachineID is the physical machine ID
	// +optional
	MachineID *string `json:"machineId,omitempty"`

	// InstanceState represents the current state of the instance
	// +optional
	InstanceState *string `json:"instanceState,omitempty"`

	// Addresses contains the IP addresses assigned to the machine
	// +optional
	Addresses []MachineAddress `json:"addresses,omitempty"`

	// Conditions represent the current state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// MachineAddress contains information for a machine's network address
type MachineAddress struct {
	// Type of the address (e.g., InternalIP, ExternalIP)
	// +required
	Type string `json:"type"`

	// Address is the IP address
	// +required
	Address string `json:"address"`
}
