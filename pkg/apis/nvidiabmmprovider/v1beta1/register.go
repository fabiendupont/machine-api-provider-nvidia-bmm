package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{
		Group:   "nvidiabmmprovider.infrastructure.cluster.x-k8s.io",
		Version: "v1beta1",
	}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	// Note: NvidiaBMMMachineProviderSpec and NvidiaBMMMachineProviderStatus are embedded
	// in OpenShift Machine objects via providerSpec.value and providerStatus.
	// They are not standalone CRDs, so we don't register them as such.
	return nil
}
