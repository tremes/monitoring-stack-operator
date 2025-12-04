package v1alpha1

// TroubleShootingSpec allows to enable and configure troubleshooting features
type TroubleShootingSpec struct {
	// ComponentHealth indicates whether the component health capability is enabled.
	// By default, it is set to false.
	// +optional
	// +kubebuilder:validation:Optional
	ComponentHealth bool `json:"componentHealth,omitempty"`
}
