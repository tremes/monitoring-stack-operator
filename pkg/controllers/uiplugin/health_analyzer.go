package uiplugin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perses/perses/go-sdk/common"
	"github.com/perses/perses/go-sdk/dashboard"
	"github.com/perses/perses/go-sdk/panel"
	panelgroup "github.com/perses/perses/go-sdk/panel-group"
	listvariable "github.com/perses/perses/go-sdk/variable/list-variable"
	"github.com/perses/plugins/prometheus/sdk/go/query"
	labelvalues "github.com/perses/plugins/prometheus/sdk/go/variable/label-values"
	table "github.com/perses/plugins/table/sdk/go"
	monv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	persesv1alpha2 "github.com/rhobs/perses-operator/api/v1alpha2"
	persesv1 "github.com/rhobs/perses/pkg/model/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	name                      = "health-analyzer"
	volumeMountName           = name + "-tls"
	componentConfigVolumeName = "components-health-config"
)

//go:embed config/health-analyzer.yaml
var componentHealthConfig string

func newHealthAnalyzerPrometheusRole(namespace string) *rbacv1.Role {
	role := &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus-k8s",
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"services", "endpoints", "pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
	return role
}

func newHealthAnalyzerPrometheusRoleBinding(namespace string) *rbacv1.RoleBinding {
	roleBinding := &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus-k8s",
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.SchemeGroupVersion.Group,
			Kind:     "Role",
			Name:     "prometheus-k8s",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "prometheus-k8s",
				Namespace: "openshift-monitoring",
			},
		},
	}
	return roleBinding
}

func newHealthAnalyzerService(namespace string) *corev1.Service {
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": volumeMountName,
			},
			Labels: componentLabels(name),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "metrics",
					Port:       8443,
					TargetPort: intstr.FromString("metrics"),
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/instance": name,
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	return service
}

func newHealthAnalyzerDeployment(namespace string,
	serviceAccountName string,
	image string) *appsv1.Deployment {

	deploy := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    componentLabels(name),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/instance": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: componentLabels(name),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           serviceAccountName,
					AutomountServiceAccountToken: ptr.To(true),
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           image,
							ImagePullPolicy: corev1.PullAlways,
							Args: []string{
								"serve",
								"--tls-cert-file=/etc/tls/private/tls.crt",
								"--tls-private-key-file=/etc/tls/private/tls.key",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "PROM_URL",
									Value: "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091/",
								},
								{
									Name:  "ALERTMANAGER_URL",
									Value: "https://alertmanager-main.openshift-monitoring.svc.cluster.local:9094",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8443,
									Name:          "metrics",
								},
							},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc/tls/private",
									Name:      volumeMountName,
									ReadOnly:  true,
								},
								{
									Name:      componentConfigVolumeName,
									MountPath: "/etc/config",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: volumeMountName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: volumeMountName,
								},
							},
						},
						{
							Name: componentConfigVolumeName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "components-config",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return deploy
}

func newHealthAnalyzerServiceMonitor(namespace string) *monv1.ServiceMonitor {
	serviceMonitor := &monv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: monv1.SchemeGroupVersion.String(),
			Kind:       "ServiceMonitor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: monv1.ServiceMonitorSpec{
			Endpoints: []monv1.Endpoint{
				{
					Interval: "30s",
					Port:     "metrics",
					Scheme:   ptr.To(monv1.Scheme("https")),
					TLSConfig: &monv1.TLSConfig{
						SafeTLSConfig: monv1.SafeTLSConfig{
							ServerName: ptr.To(name + "." + namespace + ".svc"),
						},
						CAFile:   "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
						CertFile: "/etc/prometheus/secrets/metrics-client-certs/tls.crt",
						KeyFile:  "/etc/prometheus/secrets/metrics-client-certs/tls.key",
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/instance": name,
				},
			},
		},
	}

	return serviceMonitor
}

func withComponentsOverviewPanel() dashboard.Option {
	return dashboard.AddPanelGroup("Component Health Overview",
		panelgroup.PanelsPerLine(1),
		panelgroup.AddPanel("Top level components",
			table.Table(
				table.WithCellSettings([]table.CellSettings{
					{
						Condition: table.Condition{
							Kind: "Value",
							Spec: map[string]interface{}{
								"value": "warning",
							},
						},
						Text:      "WARNING",
						TextColor: "#ffb700",
					},
					{
						Condition: table.Condition{
							Kind: "Value",
							Spec: map[string]interface{}{
								"value": "error",
							},
						},
						Text:      "ERROR",
						TextColor: "#ff0000",
					},
					{
						Condition: table.Condition{
							Kind: "Value",
							Spec: map[string]interface{}{
								"value": "OK",
							},
						},
						Text:      "OK",
						TextColor: "#23c200",
					},
				}),
				table.WithColumnSettings([]table.ColumnSettings{
					{
						Name: "timestamp",
						Hide: true,
					},
					{
						Name: "value",
						Hide: true,
					},
				}),
				table.WithDensity("comfortable"),
			),
			panel.AddQuery(
				query.PromQL(
					"sum without(job,instance,container,endpoint,namespace,pod,prometheus,service) (component_health)",
					query.SeriesNameFormat("{{component}}"),
				),
			),
		),
	)
}

func withComponentDetailsPanel() dashboard.Option {
	return dashboard.AddPanelGroup("Component Details",
		panelgroup.PanelsPerLine(1),
		panelgroup.AddPanel("Component Details: ${component}",
			table.Table(
				table.Transform([]common.Transform{
					{
						Kind: common.MergeByColumnsKind,
						Spec: common.MergeColumnsSpec{
							Columns: []string{"name", "src_alertname"},
							Name:    "name",
						},
					},
				}),
				table.WithCellSettings([]table.CellSettings{
					{
						Condition: table.Condition{
							Kind: "Value",
							Spec: map[string]interface{}{
								"value": "warning",
							},
						},
						Text:      "WARNING",
						TextColor: "#ffb700",
					},
					{
						Condition: table.Condition{
							Kind: "Value",
							Spec: map[string]interface{}{
								"value": "error",
							},
						},
						Text:      "ERROR",
						TextColor: "#ff0000",
					},
					{
						Condition: table.Condition{
							Kind: "Value",
							Spec: map[string]interface{}{
								"value": "OK",
							},
						},
						Text:      "OK",
						TextColor: "#23c200",
					},
				}),
				table.WithColumnSettings([]table.ColumnSettings{
					{
						Name: "timestamp",
						Hide: true,
					},
					{
						Name: "value",
						Hide: true,
					},
					{
						Name: "component",
					},
					{
						Name: "name",
					},
					{
						Name: "resource",
					},
					{
						Name: "progressing",
					},
					{
						Name: "status",
					},
				}),
			),
			panel.AddQuery(
				query.PromQL(
					"sum by(component,name,progressing,resource,status,src_alertname) (component_health_object{component=~\"${component}.*\"} or component_health_alert{component=~\"${component}.*\"})",
				),
			),
		),
	)
}

func buildComponentHealthDashboard() (dashboard.Builder, error) {
	return dashboard.New("component-health-dashboard",
		dashboard.Name("Component Health Dashboard"),
		dashboard.Duration(time.Hour),
		dashboard.RefreshInterval(30*time.Second),
		dashboard.AddVariable("component",
			listvariable.List(
				listvariable.DisplayName("Component Filter"),
				listvariable.Description("Select a component to view detailed health information. Use 'All Components' to see everything."),
				listvariable.Hidden(false),
				listvariable.DefaultValue("$__all"),
				listvariable.AllowAllValue(true),
				listvariable.AllowMultiple(false),
				labelvalues.PrometheusLabelValues("component",
					labelvalues.Matchers("component_health"),
				),
			),
		),
		withComponentsOverviewPanel(),
		withComponentDetailsPanel(),
	)
}

func newComponentHealthDashboard(namespace string) (*persesv1alpha2.PersesDashboard, error) {
	builder, err := buildComponentHealthDashboard()
	if err != nil {
		return nil, fmt.Errorf("failed to build component health dashboard: %w", err)
	}

	// Workaround because of type conflict between Perses plugin types and Perses fork in rhobs org
	rhobsDashboard := persesv1.Dashboard{}
	bytes, err := json.Marshal(builder.Dashboard)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dashboard: %w", err)
	}
	err = rhobsDashboard.UnmarshalJSON(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal dashboard: %w", err)
	}

	return &persesv1alpha2.PersesDashboard{
		TypeMeta: metav1.TypeMeta{
			APIVersion: persesv1alpha2.GroupVersion.String(),
			Kind:       "PersesDashboard",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "component-health-dashboard",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "observability-operator",
			},
		},
		Spec: persesv1alpha2.PersesDashboardSpec{
			Config: persesv1alpha2.Dashboard{
				DashboardSpec: rhobsDashboard.Spec,
			},
		},
	}, nil
}

// newComponentHealthConfig creates a new ConfigMap
// that defines the components whose health is evaluated.
func newComponentHealthConfig(namespace string) *v1.ConfigMap {
	cm := v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "components-config",
			Labels:    componentLabels("monitoring"),
		},
		Data: map[string]string{
			"components.yaml": componentHealthConfig,
		},
	}

	return &cm
}
