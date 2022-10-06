package coreosrhobs

import (
	"context"

	"github.com/go-logr/logr"
	stack "github.com/rhobs/observability-operator/pkg/apis/monitoring/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	coreOSGV  = schema.GroupVersion{Group: "monitoring.coreos.com", Version: "v1"}
	rhobsGV   = schema.GroupVersion{Group: "monitoring.rhobs", Version: "v1"}
	coreOSGVR = schema.GroupVersionResource{Group: coreOSGV.Group, Version: coreOSGV.Version, Resource: "servicemonitors"}
	rhobsGVR  = schema.GroupVersionResource{Group: rhobsGV.Group, Version: rhobsGV.Version, Resource: "servicemonitors"}
)

type resourceManager struct {
	dynamicCli dynamic.Interface
	scheme     *runtime.Scheme
	logger     logr.Logger
}

// RBAC for listing servicemonitors
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list
//+kubebuilder:rbac:groups="monitoring.rhobs",resources=servicemonitors,verbs=create

func RegisterWithManager(mgr ctrl.Manager) error {
	logger := ctrl.Log.WithName("coreos-rhobs")

	dynamicCli, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}
	//genChangedPred := predicate.GenerationChangedPredicate{}
	rm := &resourceManager{
		dynamicCli: dynamicCli,
		scheme:     mgr.GetScheme(),
		logger:     logger,
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&stack.MonitoringStack{}).
		//Owns(&monv1.ServiceMonitor{}).WithEventFilter(genChangedPred).
		Complete(rm)
}

func (rm *resourceManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := rm.logger.WithValues("stack", req.NamespacedName)
	uServMonList, err := rm.dynamicCli.Resource(coreOSGVR).List(ctx, v1.ListOptions{})
	if err != nil {
		logger.Error(err, "Failed to list ServiceMonitor resources")
	}

	for i := range uServMonList.Items {
		coreOsSM := uServMonList.Items[i]
		rhobsSM, err := rm.translateServiceMonitor(ctx, coreOsSM)
		if err != nil {
			logger.Error(err, "Failed to create a service monitor")
			continue
		}
		logger.Info("Created a new service monitor", rhobsSM.GetName(), rhobsGV)
	}
	return ctrl.Result{}, nil
}

func (rm *resourceManager) translateServiceMonitor(ctx context.Context, uSm unstructured.Unstructured) (*unstructured.Unstructured, error) {
	uSm.SetAPIVersion(rhobsGV.String())
	unstructured.RemoveNestedField(uSm.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(uSm.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(uSm.Object, "metadata", "generation")
	unstructured.RemoveNestedField(uSm.Object, "metadata", "uid")

	rhobsSM, err := rm.dynamicCli.Resource(rhobsGVR).Namespace(uSm.GetNamespace()).Create(ctx, &uSm, v1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return rhobsSM, nil
}
