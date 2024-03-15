package status

import (
	monv1 "github.com/rhobs/obo-prometheus-operator/pkg/apis/monitoring/v1"
)

type promConditions []monv1.Condition

// Operand is a wrapper type around client.Object
// It is a helper type to evaluate status condtions
// in generic fashion
type Operand[T monv1.Condition] struct {
	Name                string
	AffectsAvailability bool
	AffectsReconciled   bool
	Conditions          promConditions
	Generation          int64
}

// getConditionByType converts the operand object to unstructured and
// then tries to find conidtion with provided type.
func (o *Operand[C]) getConditionByType(ctype string) *monv1.Condition {
	for _, c := range o.Conditions {
		if string(c.Type) == ctype {
			return &c
		}
	}
	return nil
}
