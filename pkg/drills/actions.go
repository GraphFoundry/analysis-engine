package drills

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

// Action defines the interface for drill execution tactics.
type Action interface {
	Execute(ctx context.Context, namespace, target string, config json.RawMessage) error
	Rollback(ctx context.Context, namespace, target string, config json.RawMessage) error
}

func getK8sClient() (*kubernetes.Clientset, error) {
	// Try in-cluster first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to default kubeconfig
		kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load k8s config: %w", err)
		}
	}
	return kubernetes.NewForConfig(config)
}

// ScaleDeploymentAction handles scaling a deployment to a target replica count.
type ScaleDeploymentAction struct {
	OriginalReplicas map[string]int32
}

func NewScaleDeploymentAction() *ScaleDeploymentAction {
	return &ScaleDeploymentAction{
		OriginalReplicas: make(map[string]int32),
	}
}

type ScaleConfig struct {
	Replicas int32 `json:"replicas"`
}

func (a *ScaleDeploymentAction) Execute(ctx context.Context, namespace, target string, config json.RawMessage) error {
	var conf ScaleConfig
	if err := json.Unmarshal(config, &conf); err != nil {
		return fmt.Errorf("invalid config for scale action: %w", err)
	}

	clientset, err := getK8sClient()
	if err != nil {
		return err
	}

	deploymentsClient := clientset.AppsV1().Deployments(namespace)

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := deploymentsClient.Get(ctx, target, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get deployment: %w", getErr)
		}

		// Save original replicas if not already saved
		key := fmt.Sprintf("%s/%s", namespace, target)
		if _, exists := a.OriginalReplicas[key]; !exists {
			if result.Spec.Replicas != nil {
				a.OriginalReplicas[key] = *result.Spec.Replicas
			} else {
				a.OriginalReplicas[key] = 1
			}
		}

		result.Spec.Replicas = &conf.Replicas
		_, updateErr := deploymentsClient.Update(ctx, result, metav1.UpdateOptions{})
		return updateErr
	})

	if err != nil {
		return fmt.Errorf("failed to scale deployment: %w", err)
	}
	return nil
}

func (a *ScaleDeploymentAction) Rollback(ctx context.Context, namespace, target string, config json.RawMessage) error {
	clientset, err := getK8sClient()
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s/%s", namespace, target)
	originalReplicas, exists := a.OriginalReplicas[key]
	if !exists {
		originalReplicas = 1
	}

	deploymentsClient := clientset.AppsV1().Deployments(namespace)

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := deploymentsClient.Get(ctx, target, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get deployment: %w", getErr)
		}

		result.Spec.Replicas = &originalReplicas
		_, updateErr := deploymentsClient.Update(ctx, result, metav1.UpdateOptions{})
		return updateErr
	})

	if err != nil {
		return fmt.Errorf("failed to rollback scale deployment: %w", err)
	}
	return nil
}

// NetworkPolicyAction handles simulating a network cut via K8s NetworkPolicy.
type NetworkPolicyAction struct{}

func NewNetworkPolicyAction() *NetworkPolicyAction {
	return &NetworkPolicyAction{}
}

func (a *NetworkPolicyAction) Execute(ctx context.Context, namespace, target string, config json.RawMessage) error {
	clientset, err := getK8sClient()
	if err != nil {
		return err
	}

	policyName := fmt.Sprintf("drill-deny-%s", target)
	policy := &v1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: namespace,
			Labels: map[string]string{
				"drill-director": "active",
			},
		},
		Spec: v1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": target,
				},
			},
			PolicyTypes: []v1.PolicyType{v1.PolicyTypeIngress, v1.PolicyTypeEgress},
		},
	}

	_, err = clientset.NetworkingV1().NetworkPolicies(namespace).Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create deny network policy: %w", err)
	}
	return nil
}

func (a *NetworkPolicyAction) Rollback(ctx context.Context, namespace, target string, config json.RawMessage) error {
	clientset, err := getK8sClient()
	if err != nil {
		return err
	}

	policyName := fmt.Sprintf("drill-deny-%s", target)
	err = clientset.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, policyName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete deny network policy: %w", err)
	}
	return nil
}

type MockAction struct {
	Message string
}

func NewMockAction(msg string) *MockAction {
	return &MockAction{Message: msg}
}

func (m *MockAction) Execute(ctx context.Context, namespace, target string, config json.RawMessage) error {
	return nil
}

func (m *MockAction) Rollback(ctx context.Context, namespace, target string, config json.RawMessage) error {
	return nil
}
