package machine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restclient "github.com/NVIDIA/carbide-rest/client"
	v1beta1 "github.com/fabiendupont/machine-api-provider-nvidia-bmm/pkg/apis/nvidiabmmprovider/v1beta1"
	"github.com/fabiendupont/machine-api-provider-nvidia-bmm/pkg/providerid"
)

// NvidiaBMMClientInterface defines the methods needed from NVIDIA BMM REST client
type NvidiaBMMClientInterface interface {
	CreateInstanceWithResponse(
		ctx context.Context, org string,
		body restclient.CreateInstanceJSONRequestBody,
		reqEditors ...restclient.RequestEditorFn,
	) (*restclient.CreateInstanceResponse, error)
	GetInstanceWithResponse(
		ctx context.Context, org string, instanceId uuid.UUID,
		params *restclient.GetInstanceParams,
		reqEditors ...restclient.RequestEditorFn,
	) (*restclient.GetInstanceResponse, error)
	DeleteInstanceWithResponse(
		ctx context.Context, org string, instanceId uuid.UUID,
		body restclient.DeleteInstanceJSONRequestBody,
		reqEditors ...restclient.RequestEditorFn,
	) (*restclient.DeleteInstanceResponse, error)
}

// Actuator implements the OpenShift Machine actuator interface
type Actuator struct {
	client        client.Client
	eventRecorder record.EventRecorder
	// For testing
	nvidiaBmmClient NvidiaBMMClientInterface
	orgName         string
}

// NewActuator creates a new machine actuator
func NewActuator(k8sClient client.Client, eventRecorder record.EventRecorder) *Actuator {
	return &Actuator{
		client:        k8sClient,
		eventRecorder: eventRecorder,
	}
}

// NewActuatorWithClient creates a new machine actuator with injected client (for testing)
func NewActuatorWithClient(
	k8sClient client.Client, eventRecorder record.EventRecorder,
	nvidiaBmmClient NvidiaBMMClientInterface, orgName string,
) *Actuator {
	return &Actuator{
		client:          k8sClient,
		eventRecorder:   eventRecorder,
		nvidiaBmmClient: nvidiaBmmClient,
		orgName:         orgName,
	}
}

// buildInstanceRequest constructs the API request body from a provider spec.
func buildInstanceRequest(
	name string,
	providerSpec *v1beta1.NvidiaBMMMachineProviderSpec,
) (restclient.CreateInstanceJSONRequestBody, error) {
	subnetUUID, err := uuid.Parse(providerSpec.SubnetID)
	if err != nil {
		return restclient.CreateInstanceJSONRequestBody{},
			fmt.Errorf("failed to parse subnet ID: %w", err)
	}

	interfaces := []restclient.InterfaceCreateRequest{
		{
			SubnetId:   &subnetUUID,
			IsPhysical: ptr(false),
		},
	}

	for _, additionalSubnet := range providerSpec.AdditionalSubnetIDs {
		addSubnetUUID, err := uuid.Parse(additionalSubnet.SubnetID)
		if err != nil {
			return restclient.CreateInstanceJSONRequestBody{},
				fmt.Errorf("failed to parse additional subnet ID: %w", err)
		}
		interfaces = append(interfaces, restclient.InterfaceCreateRequest{
			SubnetId:   &addSubnetUUID,
			IsPhysical: ptr(additionalSubnet.IsPhysical),
		})
	}

	tenantUUID, err := uuid.Parse(providerSpec.TenantID)
	if err != nil {
		return restclient.CreateInstanceJSONRequestBody{},
			fmt.Errorf("failed to parse tenant ID: %w", err)
	}
	vpcUUID, err := uuid.Parse(providerSpec.VpcID)
	if err != nil {
		return restclient.CreateInstanceJSONRequestBody{},
			fmt.Errorf("failed to parse VPC ID: %w", err)
	}

	req := restclient.CreateInstanceJSONRequestBody{
		Name:             name,
		TenantId:         tenantUUID,
		VpcId:            vpcUUID,
		Interfaces:       &interfaces,
		PhoneHomeEnabled: ptr(true),
	}

	if providerSpec.InstanceTypeID != "" {
		instanceTypeUUID, err := uuid.Parse(providerSpec.InstanceTypeID)
		if err != nil {
			return restclient.CreateInstanceJSONRequestBody{},
				fmt.Errorf("failed to parse instance type ID: %w", err)
		}
		req.InstanceTypeId = &instanceTypeUUID
	}
	if providerSpec.MachineID != "" {
		req.MachineId = ptr(providerSpec.MachineID)
	}
	if providerSpec.AllowUnhealthyMachine {
		req.AllowUnhealthyMachine = ptr(true)
	}
	if providerSpec.UserData != "" {
		req.UserData = ptr(providerSpec.UserData)
	}
	if len(providerSpec.SSHKeyGroupIDs) > 0 {
		sshKeyGroupUUIDs := make([]uuid.UUID, 0, len(providerSpec.SSHKeyGroupIDs))
		for _, keyGroupID := range providerSpec.SSHKeyGroupIDs {
			keyGroupUUID, err := uuid.Parse(keyGroupID)
			if err != nil {
				return restclient.CreateInstanceJSONRequestBody{},
					fmt.Errorf("failed to parse SSH key group ID: %w", err)
			}
			sshKeyGroupUUIDs = append(sshKeyGroupUUIDs, keyGroupUUID)
		}
		req.SshKeyGroupIds = &sshKeyGroupUUIDs
	}
	if len(providerSpec.Labels) > 0 {
		labels := restclient.Labels(providerSpec.Labels)
		req.Labels = &labels
	}

	return req, nil
}

// Create provisions a new instance
func (a *Actuator) Create(ctx context.Context, machine runtime.Object) error {
	machineObj, ok := machine.(client.Object)
	if !ok {
		return fmt.Errorf("machine is not a client.Object")
	}

	// Parse provider spec
	providerSpec, err := a.getProviderSpec(machineObj)
	if err != nil {
		return fmt.Errorf("failed to get provider spec: %w", err)
	}

	// Get NVIDIA BMM client and orgName
	nvidiaBmmClient, orgName, err := a.getNvidiaBmmClient(ctx, providerSpec)
	if err != nil {
		return fmt.Errorf("failed to create NVIDIA BMM client: %w", err)
	}

	// Build instance request
	instanceReq, err := buildInstanceRequest(machineObj.GetName(), providerSpec)
	if err != nil {
		return err
	}

	// Create instance
	resp, err := nvidiaBmmClient.CreateInstanceWithResponse(ctx, orgName, instanceReq)
	if err != nil {
		if a.eventRecorder != nil {
			a.eventRecorder.Eventf(machineObj, corev1.EventTypeWarning, "FailedCreate", "Failed to create instance: %v", err)
		}
		return fmt.Errorf("failed to create instance: %w", err)
	}

	if resp.JSON201 == nil {
		if a.eventRecorder != nil {
			a.eventRecorder.Eventf(machineObj, corev1.EventTypeWarning, "FailedCreate", "Create instance returned no data")
		}
		return fmt.Errorf("create instance returned no data, status code: %d", resp.StatusCode())
	}

	instance := resp.JSON201

	// Build provider status
	providerStatus := &v1beta1.NvidiaBMMMachineProviderStatus{
		InstanceID: ptr(instance.Id.String()),
	}

	if instance.MachineId != nil {
		providerStatus.MachineID = instance.MachineId
	}
	if instance.Status != nil {
		status := string(*instance.Status)
		providerStatus.InstanceState = &status
	}

	// Extract addresses - note the API uses IpAddresses (plural, array)
	if instance.Interfaces != nil {
		for _, iface := range *instance.Interfaces {
			if iface.IpAddresses != nil {
				for _, ipAddr := range *iface.IpAddresses {
					providerStatus.Addresses = append(providerStatus.Addresses, v1beta1.MachineAddress{
						Type:    "InternalIP",
						Address: ipAddr,
					})
				}
			}
		}
	}

	if err := a.setProviderStatus(machineObj, providerStatus); err != nil {
		return fmt.Errorf("failed to update provider status: %w", err)
	}

	// Set provider ID using the local providerid package
	pid := providerid.NewProviderID(orgName, providerSpec.TenantID, providerSpec.SiteID, *instance.Id)
	if err := a.setProviderID(machineObj, pid.String()); err != nil {
		return fmt.Errorf("failed to set provider ID: %w", err)
	}

	if a.eventRecorder != nil {
		a.eventRecorder.Eventf(machineObj, corev1.EventTypeNormal, "Created", "Created instance %s", instance.Id.String())
	}
	return nil
}

// Update updates an existing instance
func (a *Actuator) Update(ctx context.Context, machine runtime.Object) error {
	machineObj, ok := machine.(client.Object)
	if !ok {
		return fmt.Errorf("machine is not a client.Object")
	}

	// Parse provider spec
	providerSpec, err := a.getProviderSpec(machineObj)
	if err != nil {
		return fmt.Errorf("failed to get provider spec: %w", err)
	}

	// Get provider status
	providerStatus, err := a.getProviderStatus(machineObj)
	if err != nil {
		return fmt.Errorf("failed to get provider status: %w", err)
	}

	if providerStatus.InstanceID == nil {
		return fmt.Errorf("instance ID not set in provider status")
	}

	// Get NVIDIA BMM client and orgName
	nvidiaBmmClient, orgName, err := a.getNvidiaBmmClient(ctx, providerSpec)
	if err != nil {
		return fmt.Errorf("failed to create NVIDIA BMM client: %w", err)
	}

	// Parse instance ID
	instanceUUID, err := uuid.Parse(*providerStatus.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to parse instance ID: %w", err)
	}

	// Get current instance status
	resp, err := nvidiaBmmClient.GetInstanceWithResponse(ctx, orgName, instanceUUID, nil)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	if resp.JSON200 == nil {
		return fmt.Errorf("get instance returned no data, status code: %d", resp.StatusCode())
	}

	instance := resp.JSON200

	// Update provider status
	if instance.Status != nil {
		status := string(*instance.Status)
		providerStatus.InstanceState = &status
	}
	if instance.MachineId != nil {
		providerStatus.MachineID = instance.MachineId
	}

	// Update addresses
	providerStatus.Addresses = []v1beta1.MachineAddress{}
	if instance.Interfaces != nil {
		for _, iface := range *instance.Interfaces {
			if iface.IpAddresses != nil {
				for _, ipAddr := range *iface.IpAddresses {
					providerStatus.Addresses = append(providerStatus.Addresses, v1beta1.MachineAddress{
						Type:    "InternalIP",
						Address: ipAddr,
					})
				}
			}
		}
	}

	if err := a.setProviderStatus(machineObj, providerStatus); err != nil {
		return fmt.Errorf("failed to update provider status: %w", err)
	}

	return nil
}

// Exists checks if instance exists
func (a *Actuator) Exists(ctx context.Context, machine runtime.Object) (bool, error) {
	machineObj, ok := machine.(client.Object)
	if !ok {
		return false, fmt.Errorf("machine is not a client.Object")
	}

	// Get provider status
	providerStatus, err := a.getProviderStatus(machineObj)
	if err != nil {
		return false, fmt.Errorf("failed to get provider status: %w", err)
	}

	if providerStatus.InstanceID == nil {
		return false, nil
	}

	// Parse provider spec
	providerSpec, err := a.getProviderSpec(machineObj)
	if err != nil {
		return false, fmt.Errorf("failed to get provider spec: %w", err)
	}

	// Get NVIDIA BMM client and orgName
	nvidiaBmmClient, orgName, err := a.getNvidiaBmmClient(ctx, providerSpec)
	if err != nil {
		return false, fmt.Errorf("failed to create NVIDIA BMM client: %w", err)
	}

	// Parse instance ID
	instanceUUID, err := uuid.Parse(*providerStatus.InstanceID)
	if err != nil {
		return false, fmt.Errorf("failed to parse instance ID: %w", err)
	}

	// Check if instance exists
	resp, err := nvidiaBmmClient.GetInstanceWithResponse(ctx, orgName, instanceUUID, nil)
	if err != nil {
		return false, nil
	}

	// Instance exists if we get a 200 response
	return resp.JSON200 != nil, nil
}

// Delete deprovisions the instance
func (a *Actuator) Delete(ctx context.Context, machine runtime.Object) error {
	machineObj, ok := machine.(client.Object)
	if !ok {
		return fmt.Errorf("machine is not a client.Object")
	}

	// Parse provider spec
	providerSpec, err := a.getProviderSpec(machineObj)
	if err != nil {
		return fmt.Errorf("failed to get provider spec: %w", err)
	}

	// Get provider status
	providerStatus, err := a.getProviderStatus(machineObj)
	if err != nil {
		return fmt.Errorf("failed to get provider status: %w", err)
	}

	if providerStatus.InstanceID == nil {
		// Nothing to delete
		return nil
	}

	// Get NVIDIA BMM client and orgName
	nvidiaBmmClient, orgName, err := a.getNvidiaBmmClient(ctx, providerSpec)
	if err != nil {
		return fmt.Errorf("failed to create NVIDIA BMM client: %w", err)
	}

	// Parse instance ID
	instanceUUID, err := uuid.Parse(*providerStatus.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to parse instance ID: %w", err)
	}

	// Delete instance
	deleteReq := restclient.InstanceDeleteRequest{}
	resp, err := nvidiaBmmClient.DeleteInstanceWithResponse(ctx, orgName, instanceUUID, deleteReq)
	if err != nil {
		if a.eventRecorder != nil {
			a.eventRecorder.Eventf(machineObj, corev1.EventTypeWarning, "FailedDelete", "Failed to delete instance: %v", err)
		}
		return fmt.Errorf("failed to delete instance: %w", err)
	}

	// Check response
	if resp.StatusCode() != 204 && resp.StatusCode() != 404 {
		if a.eventRecorder != nil {
			a.eventRecorder.Eventf(machineObj, corev1.EventTypeWarning, "FailedDelete",
				"Delete instance returned unexpected status: %d", resp.StatusCode())
		}
		return fmt.Errorf("delete instance returned unexpected status: %d", resp.StatusCode())
	}

	if a.eventRecorder != nil {
		a.eventRecorder.Eventf(machineObj, corev1.EventTypeNormal, "Deleted",
			"Deleted instance %s", *providerStatus.InstanceID)
	}
	return nil
}

// Helper functions

func (a *Actuator) getProviderSpec(machine client.Object) (*v1beta1.NvidiaBMMMachineProviderSpec, error) {
	// Cast to unstructured to access nested fields
	unstructuredMachine, ok := machine.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("machine is not unstructured")
	}

	// Extract providerSpec.value from spec
	providerSpecValue, found, err := unstructured.NestedFieldCopy(
		unstructuredMachine.Object,
		"spec", "providerSpec", "value",
	)
	if err != nil || !found {
		return nil, fmt.Errorf("providerSpec.value not found: %w", err)
	}

	// Marshal and unmarshal to get typed struct
	providerSpecBytes, err := json.Marshal(providerSpecValue)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal providerSpec: %w", err)
	}

	providerSpec := &v1beta1.NvidiaBMMMachineProviderSpec{}
	if err := json.Unmarshal(providerSpecBytes, providerSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal providerSpec: %w", err)
	}

	return providerSpec, nil
}

func (a *Actuator) getProviderStatus(machine client.Object) (*v1beta1.NvidiaBMMMachineProviderStatus, error) {
	// Cast to unstructured to access nested fields
	unstructuredMachine, ok := machine.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("machine is not unstructured")
	}

	// Extract providerStatus from status
	providerStatusValue, found, err := unstructured.NestedFieldCopy(
		unstructuredMachine.Object,
		"status", "providerStatus",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get providerStatus: %w", err)
	}

	// If not found, return empty status (this is OK for new machines)
	if !found {
		return &v1beta1.NvidiaBMMMachineProviderStatus{}, nil
	}

	// Marshal and unmarshal to get typed struct
	providerStatusBytes, err := json.Marshal(providerStatusValue)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal providerStatus: %w", err)
	}

	providerStatus := &v1beta1.NvidiaBMMMachineProviderStatus{}
	if err := json.Unmarshal(providerStatusBytes, providerStatus); err != nil {
		return nil, fmt.Errorf("failed to unmarshal providerStatus: %w", err)
	}

	return providerStatus, nil
}

func (a *Actuator) setProviderStatus(machine client.Object, status *v1beta1.NvidiaBMMMachineProviderStatus) error {
	// Cast to unstructured to access nested fields
	unstructuredMachine, ok := machine.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("machine is not unstructured")
	}

	// Convert status to map
	statusBytes, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	var statusMap map[string]interface{}
	if err := json.Unmarshal(statusBytes, &statusMap); err != nil {
		return fmt.Errorf("failed to unmarshal status to map: %w", err)
	}

	// Set providerStatus in status
	if err := unstructured.SetNestedField(
		unstructuredMachine.Object,
		statusMap,
		"status", "providerStatus",
	); err != nil {
		return fmt.Errorf("failed to set providerStatus: %w", err)
	}

	// Update the machine status
	if err := a.client.Status().Update(context.Background(), unstructuredMachine); err != nil {
		return fmt.Errorf("failed to update machine status: %w", err)
	}

	return nil
}

func (a *Actuator) setProviderID(machine client.Object, providerID string) error {
	// Cast to unstructured to access nested fields
	unstructuredMachine, ok := machine.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("machine is not unstructured")
	}

	// Set spec.providerID
	if err := unstructured.SetNestedField(
		unstructuredMachine.Object,
		providerID,
		"spec", "providerID",
	); err != nil {
		return fmt.Errorf("failed to set providerID: %w", err)
	}

	// Update the machine
	if err := a.client.Update(context.Background(), unstructuredMachine); err != nil {
		return fmt.Errorf("failed to update machine: %w", err)
	}

	return nil
}

func (a *Actuator) getNvidiaBmmClient(
	ctx context.Context, providerSpec *v1beta1.NvidiaBMMMachineProviderSpec,
) (NvidiaBMMClientInterface, string, error) {
	// Use injected client for testing
	if a.nvidiaBmmClient != nil {
		return a.nvidiaBmmClient, a.orgName, nil
	}

	// Fetch credentials secret
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Name:      providerSpec.CredentialsSecret.Name,
		Namespace: providerSpec.CredentialsSecret.Namespace,
	}

	if err := a.client.Get(ctx, secretKey, secret); err != nil {
		return nil, "", fmt.Errorf("failed to get credentials secret: %w", err)
	}

	// Validate secret contains required fields
	endpoint, ok := secret.Data["endpoint"]
	if !ok {
		return nil, "", fmt.Errorf("secret %s is missing 'endpoint' field", secretKey.Name)
	}
	orgName, ok := secret.Data["orgName"]
	if !ok {
		return nil, "", fmt.Errorf("secret %s is missing 'orgName' field", secretKey.Name)
	}
	token, ok := secret.Data["token"]
	if !ok {
		return nil, "", fmt.Errorf("secret %s is missing 'token' field", secretKey.Name)
	}

	// Create NVIDIA BMM API client using the REST client
	bmmClient, err := restclient.NewClientWithAuth(string(endpoint), string(token))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create NVIDIA BMM client: %w", err)
	}

	return bmmClient, string(orgName), nil
}

// ptr is a helper function to get a pointer to a value
func ptr[T any](v T) *T {
	return &v
}
