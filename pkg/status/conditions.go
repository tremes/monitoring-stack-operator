package status

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	monv1 "github.com/rhobs/obo-prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rhobs/observability-operator/pkg/apis/shared"
)

const (
	AvailableReason               = "MonitoringStackAvailable"
	ReconciledReason              = "MonitoringStackReconciled"
	FailedToReconcileReason       = "FailedToReconcile"
	ResourceSelectorIsNil         = "ResourceSelectorNil"
	AvailableMessage              = "Monitoring Stack is available"
	SuccessfullyReconciledMessage = "Monitoring Stack is successfully reconciled"
	ResourceSelectorIsNilMessage  = "No resources will be discovered, ResourceSelector is nil"
	ResourceDiscoveryOnMessage    = "Resource discovery is operational"
	NoReason                      = "None"
	available                     = "Available"
	reconciled                    = "Reconciled"
)

func UpdateConditions(stackObj shared.StatusReporter, operands []Operand[monv1.Condition], recError error) ([]shared.Condition, error) {
	var availableCon shared.Condition
	var reconciledCon shared.Condition
	conditions := stackObj.Conditions()
	for _, opr := range operands {
		if opr.AffectsAvailability {
			availableCon = updateAvailable(conditions, opr, stackObj.GetGeneration())
		}
		if opr.AffectsReconciled {
			reconciledCon = updateReconciled(conditions, opr, stackObj.GetGeneration(), recError)
		}
	}

	resourceDiscoveryCon, err := updateResourceDiscovery(stackObj)
	if err != nil {
		return nil, err
	}

	return []shared.Condition{
		availableCon,
		reconciledCon,
		*resourceDiscoveryCon,
	}, nil
}

// updateResourceDiscovery updates the ResourceDiscoveryCondition based on the
// ResourceSelector in the MonitorinStack spec. A ResourceSelector of nil causes
// the condition to be false, any other value sets the condition to true
func updateResourceDiscovery(stackObj shared.StatusReporter) (*shared.Condition, error) {
	unstrObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(stackObj)
	if err != nil {
		return nil, err
	}
	rs, ok, err := unstructured.NestedFieldCopy(unstrObj, "spec", "resourceSelector")
	if err != nil {
		return nil, err
	}
	if rs == nil || !ok {
		return &shared.Condition{
			Type:               shared.ResourceDiscoveryCondition,
			Status:             shared.ConditionFalse,
			Reason:             ResourceSelectorIsNil,
			Message:            ResourceSelectorIsNilMessage,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: stackObj.GetGeneration(),
		}, nil
	} else {
		return &shared.Condition{
			Type:               shared.ResourceDiscoveryCondition,
			Status:             shared.ConditionTrue,
			Reason:             NoReason,
			Message:            ResourceDiscoveryOnMessage,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: stackObj.GetGeneration(),
		}, nil
	}

}

// updateAvailable gets existing "Available" condition and updates its parameters
// based on the operand "Available" condition
func updateAvailable(conditions []shared.Condition, opr Operand[monv1.Condition], generation int64) shared.Condition {
	ac, err := getConditionByType(conditions, shared.AvailableCondition)
	if err != nil {
		ac = shared.Condition{
			Type:               shared.AvailableCondition,
			Status:             shared.ConditionUnknown,
			Reason:             NoReason,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
		return ac
	}

	operandAvailable := opr.getConditionByType(available)
	if operandAvailable == nil {
		ac.Status = shared.ConditionUnknown
		ac.Reason = fmt.Sprintf("%sNotAvailable", opr.Name)
		ac.Message = fmt.Sprintf("Cannot read %s status conditions", opr.Name)
		ac.LastTransitionTime = metav1.Now()
		return ac
	}
	// MonitoringStack status will not be updated if there is a difference between the operand generation
	// and the operand ObservedGeneration. This can occur, for example, in the case of an invalid operand configuration.
	if operandAvailable.ObservedGeneration != opr.Generation {
		return ac
	}

	if operandAvailable.Status != monv1.ConditionTrue {
		ac.Status = prometheusStatusToMSStatus(operandAvailable.Status)
		if operandAvailable.Status == monv1.ConditionDegraded {
			ac.Reason = fmt.Sprintf("%sDegraded", opr.Name)
		} else {
			ac.Reason = fmt.Sprintf("%sNotAvailable", opr.Name)
		}
		ac.Message = operandAvailable.Message
		ac.LastTransitionTime = metav1.Now()
		return ac
	}
	ac.Status = shared.ConditionTrue
	ac.Reason = AvailableReason
	ac.Message = AvailableMessage
	ac.ObservedGeneration = generation
	ac.LastTransitionTime = metav1.Now()
	return ac
}

// updateReconciled updates "Reconciled" conditions based on the provided error value and
// the operand "Reconciled" condition
func updateReconciled(conditions []shared.Condition, opr Operand[monv1.Condition], generation int64, reconcileErr error) shared.Condition {
	rc, cErr := getConditionByType(conditions, shared.ReconciledCondition)
	if cErr != nil {
		rc = shared.Condition{
			Type:               shared.ReconciledCondition,
			Status:             shared.ConditionUnknown,
			Reason:             NoReason,
			LastTransitionTime: metav1.Now(),
			Message:            cErr.Error(),
		}
		return rc
	}
	if reconcileErr != nil {
		rc.Status = shared.ConditionFalse
		rc.Message = reconcileErr.Error()
		rc.Reason = FailedToReconcileReason
		rc.LastTransitionTime = metav1.Now()
		return rc
	}
	operandReconciled := opr.getConditionByType(reconciled)

	if operandReconciled == nil {
		rc.Status = shared.ConditionUnknown
		rc.Reason = fmt.Sprintf("%sNotReconciled", opr.Name)
		rc.Message = fmt.Sprintf("Cannot read %s status conditions", opr.Name)
		rc.LastTransitionTime = metav1.Now()
		return rc
	}

	if operandReconciled.ObservedGeneration != opr.Generation {
		return rc
	}

	if operandReconciled.Status != "True" {
		rc.Status = prometheusStatusToMSStatus(operandReconciled.Status)
		rc.Reason = fmt.Sprintf("%sNotReconciled", opr.Name)
		rc.Message = operandReconciled.Message
		rc.LastTransitionTime = metav1.Now()
		return rc
	}
	rc.Status = shared.ConditionTrue
	rc.Reason = ReconciledReason
	rc.Message = SuccessfullyReconciledMessage
	rc.ObservedGeneration = generation
	rc.LastTransitionTime = metav1.Now()
	return rc
}

func getConditionByType(conditions []shared.Condition, t shared.ConditionType) (shared.Condition, error) {
	for _, c := range conditions {
		if c.Type == t {
			return c, nil
		}
	}
	return shared.Condition{}, fmt.Errorf("condition type %v not found", t)
}

func prometheusStatusToMSStatus(ps monv1.ConditionStatus) shared.ConditionStatus {
	switch ps {
	// Prometheus "Available" condition with status "Degraded" is reported as "Available" condition
	// with status false
	case monv1.ConditionDegraded:
		return shared.ConditionFalse
	case monv1.ConditionTrue:
		return shared.ConditionTrue
	case monv1.ConditionFalse:
		return shared.ConditionFalse
	case monv1.ConditionUnknown:
		return shared.ConditionUnknown
	default:
		return shared.ConditionUnknown
	}
}
