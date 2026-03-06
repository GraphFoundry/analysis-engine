package drills

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

type K8sClientOptions struct {
	KubeconfigPath string
	KubeContext    string
	APIServer      string
}

type K8sClientFactory struct {
	opts K8sClientOptions
}

func NewK8sClientFactory(opts K8sClientOptions) *K8sClientFactory {
	return &K8sClientFactory{opts: opts}
}

func (f *K8sClientFactory) Clientset() (*kubernetes.Clientset, error) {
	restCfg, err := f.restConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restCfg)
}

func (f *K8sClientFactory) PreflightConnectivity(ctx context.Context) error {
	clientset, restCfg, err := f.clientsetWithConfig()
	if err != nil {
		return wrapK8sPreflightError("load kubernetes client configuration", "", err)
	}
	if _, err := clientset.Discovery().ServerVersion(); err != nil {
		return wrapK8sPreflightError("probe kubernetes api server", restCfg.Host, err)
	}
	return nil
}

func (f *K8sClientFactory) PreflightDeploymentAccess(ctx context.Context, namespace, deployment string) error {
	clientset, restCfg, err := f.clientsetWithConfig()
	if err != nil {
		return wrapK8sPreflightError("load kubernetes client configuration", "", err)
	}
	if _, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{}); err != nil {
		resource := fmt.Sprintf("read deployment %s/%s", namespace, deployment)
		return wrapK8sPreflightError(resource, restCfg.Host, err)
	}
	return nil
}

func (f *K8sClientFactory) PreflightNamespaceAccess(ctx context.Context, namespace string) error {
	clientset, restCfg, err := f.clientsetWithConfig()
	if err != nil {
		return wrapK8sPreflightError("load kubernetes client configuration", "", err)
	}
	if _, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		resource := fmt.Sprintf("read pods in namespace %s", namespace)
		return wrapK8sPreflightError(resource, restCfg.Host, err)
	}
	return nil
}

func (f *K8sClientFactory) clientsetWithConfig() (*kubernetes.Clientset, *rest.Config, error) {
	restCfg, err := f.restConfig()
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, err
	}
	return clientset, restCfg, nil
}

func (f *K8sClientFactory) restConfig() (*rest.Config, error) {
	opts := f.resolvedOptions()

	var inClusterErr error
	var kubeconfigErr error

	kubeconfigPreferred := opts.KubeconfigPath != "" || opts.KubeContext != "" || os.Getenv("KUBECONFIG") != ""
	if kubeconfigPreferred {
		if cfg, err := buildKubeconfigRestConfig(opts); err == nil {
			return cfg, nil
		} else {
			kubeconfigErr = err
		}
	}

	if cfg, err := rest.InClusterConfig(); err == nil {
		if opts.APIServer != "" {
			cfg.Host = opts.APIServer
		}
		return cfg, nil
	} else {
		inClusterErr = err
	}

	if !kubeconfigPreferred {
		if cfg, err := buildKubeconfigRestConfig(opts); err == nil {
			return cfg, nil
		} else {
			kubeconfigErr = err
		}
	}

	return nil, fmt.Errorf("failed to load k8s config (in-cluster: %v; kubeconfig: %v)", inClusterErr, kubeconfigErr)
}

func (f *K8sClientFactory) resolvedOptions() K8sClientOptions {
	opts := K8sClientOptions{}
	if f != nil {
		opts = f.opts
	}
	if opts.KubeconfigPath == "" {
		opts.KubeconfigPath = firstNonEmptyEnv("DRILLS_KUBECONFIG_PATH", "DRILLS_KUBECONFIG")
	}
	if opts.KubeContext == "" {
		opts.KubeContext = os.Getenv("DRILLS_KUBE_CONTEXT")
	}
	if opts.APIServer == "" {
		opts.APIServer = os.Getenv("DRILLS_KUBE_API_SERVER")
	}
	return opts
}

func buildKubeconfigRestConfig(opts K8sClientOptions) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.KubeconfigPath != "" {
		loadingRules.ExplicitPath = expandHomePath(opts.KubeconfigPath)
	}

	overrides := &clientcmd.ConfigOverrides{}
	if opts.KubeContext != "" {
		overrides.CurrentContext = opts.KubeContext
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	cfg, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	if opts.APIServer != "" {
		cfg.Host = opts.APIServer
	}
	return cfg, nil
}

func wrapK8sPreflightError(operation, apiHost string, err error) error {
	if err == nil {
		return nil
	}
	base := fmt.Sprintf("drill preflight failed: unable to %s", operation)
	if apiHost != "" {
		base = fmt.Sprintf("%s via %s", base, apiHost)
	}
	if isLoopbackAPIHost(apiHost) && strings.Contains(strings.ToLower(err.Error()), "connect: connection refused") {
		return fmt.Errorf(
			"%s: %w (detected loopback kubernetes api endpoint; if this environment relies on an SSH tunnel, start the tunnel on the analysis-engine host or set DRILLS_KUBE_API_SERVER / DRILLS_KUBECONFIG_PATH to a reachable cluster endpoint before calling /drills/run)",
			base,
			err,
		)
	}
	return fmt.Errorf("%s: %w", base, err)
}

func isLoopbackAPIHost(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func expandHomePath(path string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

// ScaleDeploymentAction handles scaling a deployment to a target replica count.
type ScaleDeploymentAction struct {
	clients          *K8sClientFactory
	OriginalReplicas map[string]int32
}

func NewScaleDeploymentAction(clients ...*K8sClientFactory) *ScaleDeploymentAction {
	var clientFactory *K8sClientFactory
	if len(clients) > 0 {
		clientFactory = clients[0]
	}
	return &ScaleDeploymentAction{
		clients:          clientFactory,
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

	clientset, err := getK8sClient(a.clients)
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
	clientset, err := getK8sClient(a.clients)
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
	delete(a.OriginalReplicas, key)
	return nil
}

// NetworkPolicyAction handles simulating a network cut via K8s NetworkPolicy.
type NetworkPolicyAction struct {
	clients          *K8sClientFactory
	restoreSnapshots map[string]networkPolicyRestoreSnapshot
}

func NewNetworkPolicyAction(clients ...*K8sClientFactory) *NetworkPolicyAction {
	var clientFactory *K8sClientFactory
	if len(clients) > 0 {
		clientFactory = clients[0]
	}
	return &NetworkPolicyAction{
		clients:          clientFactory,
		restoreSnapshots: make(map[string]networkPolicyRestoreSnapshot),
	}
}

func (a *NetworkPolicyAction) Execute(ctx context.Context, namespace, target string, config json.RawMessage) error {
	clientset, err := getK8sClient(a.clients)
	if err != nil {
		return err
	}

	policyName := fmt.Sprintf("drill-deny-%s", target)
	stateKey := fmt.Sprintf("%s/%s", namespace, target)

	preSnapshotNames, err := listNetworkPolicyNames(ctx, clientset, namespace)
	if err != nil {
		return fmt.Errorf("failed to capture pre network policy snapshot: %w", err)
	}
	if containsString(preSnapshotNames, policyName) {
		return fmt.Errorf("network cut blocked: policy %q already exists before drill (pre-snapshot count=%d)", policyName, len(preSnapshotNames))
	}

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
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("network cut blocked: policy %q appeared during action execution", policyName)
		}
		return fmt.Errorf("failed to create deny network policy: %w", err)
	}

	createdPolicy, err := clientset.NetworkingV1().NetworkPolicies(namespace).Get(ctx, policyName, metav1.GetOptions{})
	if err != nil {
		_ = clientset.NetworkingV1().NetworkPolicies(namespace).Delete(context.Background(), policyName, metav1.DeleteOptions{})
		return fmt.Errorf("network cut create verification failed for %q: %w", policyName, err)
	}
	if createdPolicy.Labels["drill-director"] != "active" {
		_ = clientset.NetworkingV1().NetworkPolicies(namespace).Delete(context.Background(), policyName, metav1.DeleteOptions{})
		return fmt.Errorf("network cut create verification failed for %q: expected drill-director=active label", policyName)
	}

	postCreateSnapshotNames, err := listNetworkPolicyNames(ctx, clientset, namespace)
	if err != nil {
		_ = clientset.NetworkingV1().NetworkPolicies(namespace).Delete(context.Background(), policyName, metav1.DeleteOptions{})
		return fmt.Errorf("failed to capture post-create network policy snapshot: %w", err)
	}
	if !containsString(postCreateSnapshotNames, policyName) {
		_ = clientset.NetworkingV1().NetworkPolicies(namespace).Delete(context.Background(), policyName, metav1.DeleteOptions{})
		return fmt.Errorf("network cut create verification failed for %q: policy missing from post-create snapshot", policyName)
	}

	a.restoreSnapshots[stateKey] = networkPolicyRestoreSnapshot{
		PolicyName: policyName,
		PreNames:   preSnapshotNames,
	}
	return nil
}

func (a *NetworkPolicyAction) Rollback(ctx context.Context, namespace, target string, config json.RawMessage) error {
	clientset, err := getK8sClient(a.clients)
	if err != nil {
		return err
	}

	stateKey := fmt.Sprintf("%s/%s", namespace, target)
	policyName := fmt.Sprintf("drill-deny-%s", target)
	snapshot, hasSnapshot := a.restoreSnapshots[stateKey]
	if !hasSnapshot {
		_, getErr := clientset.NetworkingV1().NetworkPolicies(namespace).Get(ctx, policyName, metav1.GetOptions{})
		if getErr == nil {
			return fmt.Errorf("cannot safely rollback network cut: missing pre-snapshot state and policy %q still exists", policyName)
		}
		if !apierrors.IsNotFound(getErr) {
			return fmt.Errorf("failed to verify network policy state without snapshot for %q: %w", policyName, getErr)
		}
		return nil
	}

	err = clientset.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, policyName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete deny network policy: %w", err)
	}

	_, getErr := clientset.NetworkingV1().NetworkPolicies(namespace).Get(ctx, policyName, metav1.GetOptions{})
	if getErr == nil {
		return fmt.Errorf("network cut restore verification failed: policy %q still exists after rollback", policyName)
	}
	if !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("network cut restore verification failed while reading %q after rollback: %w", policyName, getErr)
	}

	postRollbackNames, err := listNetworkPolicyNames(ctx, clientset, namespace)
	if err != nil {
		return fmt.Errorf("failed to capture post-rollback network policy snapshot: %w", err)
	}

	if !stringSlicesEqual(snapshot.PreNames, postRollbackNames) {
		missing, extra := diffStringSets(snapshot.PreNames, postRollbackNames)
		return fmt.Errorf(
			"network cut restore verification failed for %q: pre_count=%d post_count=%d missing=%v extra=%v",
			policyName,
			len(snapshot.PreNames),
			len(postRollbackNames),
			truncateStrings(missing, 6),
			truncateStrings(extra, 6),
		)
	}

	delete(a.restoreSnapshots, stateKey)
	return nil
}

type networkPolicyRestoreSnapshot struct {
	PolicyName string
	PreNames   []string
}

type TargetedLoadActionOptions struct {
	DeploymentName string
	ContainerName  string
	RateEnvName    string
	UsersEnvName   string
}

func DefaultTargetedLoadActionOptions() TargetedLoadActionOptions {
	return TargetedLoadActionOptions{
		DeploymentName: "loadgenerator",
		ContainerName:  "main",
		RateEnvName:    "RATE",
		UsersEnvName:   "USERS",
	}
}

func (o TargetedLoadActionOptions) withDefaults() TargetedLoadActionOptions {
	d := DefaultTargetedLoadActionOptions()
	if o.DeploymentName != "" {
		d.DeploymentName = o.DeploymentName
	}
	if o.ContainerName != "" {
		d.ContainerName = o.ContainerName
	}
	if o.RateEnvName != "" {
		d.RateEnvName = o.RateEnvName
	}
	if o.UsersEnvName != "" {
		d.UsersEnvName = o.UsersEnvName
	}
	return d
}

type TargetedLoadConfig struct {
	RPS   int `json:"rps"`
	Rate  int `json:"rate,omitempty"`
	Users int `json:"users,omitempty"`
}

type envVarSnapshot struct {
	Exists bool
	Value  string
}

type targetedLoadOriginalState struct {
	Rate  envVarSnapshot
	Users envVarSnapshot
}

type TargetedLoadAction struct {
	clients   *K8sClientFactory
	opts      TargetedLoadActionOptions
	originals map[string]targetedLoadOriginalState
}

func NewTargetedLoadAction(opts TargetedLoadActionOptions, clients ...*K8sClientFactory) *TargetedLoadAction {
	var clientFactory *K8sClientFactory
	if len(clients) > 0 {
		clientFactory = clients[0]
	}
	return &TargetedLoadAction{
		clients:   clientFactory,
		opts:      opts.withDefaults(),
		originals: make(map[string]targetedLoadOriginalState),
	}
}

func (a *TargetedLoadAction) Execute(ctx context.Context, namespace, target string, config json.RawMessage) error {
	var conf TargetedLoadConfig
	if err := json.Unmarshal(config, &conf); err != nil {
		return fmt.Errorf("invalid config for targeted load action: %w", err)
	}

	desiredRate := conf.Rate
	if desiredRate <= 0 {
		desiredRate = conf.RPS
	}
	if desiredRate <= 0 {
		return fmt.Errorf("invalid targeted load rate: expected positive rps/rate")
	}

	clientset, err := getK8sClient(a.clients)
	if err != nil {
		return err
	}

	deploymentsClient := clientset.AppsV1().Deployments(namespace)
	stateKey := fmt.Sprintf("%s/%s", namespace, a.opts.DeploymentName)

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(ctx, a.opts.DeploymentName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get load generator deployment %q: %w", a.opts.DeploymentName, getErr)
		}

		containerIdx, findErr := findContainerIndex(deployment, a.opts.ContainerName)
		if findErr != nil {
			return findErr
		}

		container := &deployment.Spec.Template.Spec.Containers[containerIdx]
		if _, exists := a.originals[stateKey]; !exists {
			rateValue, rateExists := getEnvVar(container.Env, a.opts.RateEnvName)
			usersValue, usersExists := getEnvVar(container.Env, a.opts.UsersEnvName)
			a.originals[stateKey] = targetedLoadOriginalState{
				Rate:  envVarSnapshot{Exists: rateExists, Value: rateValue},
				Users: envVarSnapshot{Exists: usersExists, Value: usersValue},
			}
		}

		container.Env = setEnvVar(container.Env, a.opts.RateEnvName, strconv.Itoa(desiredRate))
		if conf.Users > 0 {
			container.Env = setEnvVar(container.Env, a.opts.UsersEnvName, strconv.Itoa(conf.Users))
		}

		_, updateErr := deploymentsClient.Update(ctx, deployment, metav1.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		return fmt.Errorf("failed to apply targeted load action: %w", err)
	}
	return nil
}

func (a *TargetedLoadAction) Rollback(ctx context.Context, namespace, target string, config json.RawMessage) error {
	stateKey := fmt.Sprintf("%s/%s", namespace, a.opts.DeploymentName)
	original, exists := a.originals[stateKey]
	if !exists {
		return nil
	}

	clientset, err := getK8sClient(a.clients)
	if err != nil {
		return err
	}

	deploymentsClient := clientset.AppsV1().Deployments(namespace)
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(ctx, a.opts.DeploymentName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get load generator deployment %q for rollback: %w", a.opts.DeploymentName, getErr)
		}

		containerIdx, findErr := findContainerIndex(deployment, a.opts.ContainerName)
		if findErr != nil {
			return findErr
		}

		container := &deployment.Spec.Template.Spec.Containers[containerIdx]
		container.Env = restoreEnvVar(container.Env, a.opts.RateEnvName, original.Rate)
		container.Env = restoreEnvVar(container.Env, a.opts.UsersEnvName, original.Users)

		_, updateErr := deploymentsClient.Update(ctx, deployment, metav1.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		return fmt.Errorf("failed to rollback targeted load action: %w", err)
	}

	delete(a.originals, stateKey)
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

func getK8sClient(factory *K8sClientFactory) (*kubernetes.Clientset, error) {
	if factory == nil {
		factory = NewK8sClientFactory(K8sClientOptions{})
	}
	return factory.Clientset()
}

// MigrateServiceAction migrates a service's pods to a specific target node.
// It uses nodeSelector patching + scale-down/up to force pod rescheduling.
type MigrateServiceAction struct {
	clients           *K8sClientFactory
	OriginalReplicas  map[string]int32
	OriginalSelector  map[string]map[string]string // saved nodeSelector for rollback
	OriginalScheduler map[string]string            // saved schedulerName for rollback
}

func NewMigrateServiceAction(clients ...*K8sClientFactory) *MigrateServiceAction {
	var clientFactory *K8sClientFactory
	if len(clients) > 0 {
		clientFactory = clients[0]
	}
	return &MigrateServiceAction{
		clients:           clientFactory,
		OriginalReplicas:  make(map[string]int32),
		OriginalSelector:  make(map[string]map[string]string),
		OriginalScheduler: make(map[string]string),
	}
}

type MigrateConfig struct {
	TargetNode string `json:"targetNode"`
	Replicas   int32  `json:"replicas,omitempty"` // if 0, preserves current replica count
}

func (a *MigrateServiceAction) Execute(ctx context.Context, namespace, target string, config json.RawMessage) error {
	var conf MigrateConfig
	if err := json.Unmarshal(config, &conf); err != nil {
		return fmt.Errorf("invalid config for migrate action: %w", err)
	}
	if conf.TargetNode == "" {
		return fmt.Errorf("targetNode is required for migration")
	}

	clientset, err := getK8sClient(a.clients)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s/%s", namespace, target)
	if err := a.saveOriginalState(ctx, clientset, namespace, target, key); err != nil {
		return err
	}
	if err := a.patchAndScaleDown(ctx, clientset, namespace, target, conf.TargetNode, key); err != nil {
		return err
	}
	if err := a.waitForPodsTerminated(ctx, clientset, namespace, target); err != nil {
		return err
	}
	return a.scaleUpOnTarget(ctx, clientset, namespace, target, conf.Replicas, key)
}

func (a *MigrateServiceAction) saveOriginalState(ctx context.Context, clientset *kubernetes.Clientset, namespace, target, key string) error {
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, target, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment for snapshot: %w", err)
	}
	if _, exists := a.OriginalReplicas[key]; !exists {
		if deployment.Spec.Replicas != nil {
			a.OriginalReplicas[key] = *deployment.Spec.Replicas
		} else {
			a.OriginalReplicas[key] = 1
		}
	}
	if _, exists := a.OriginalSelector[key]; !exists {
		if deployment.Spec.Template.Spec.NodeSelector != nil {
			orig := make(map[string]string)
			for k, v := range deployment.Spec.Template.Spec.NodeSelector {
				orig[k] = v
			}
			a.OriginalSelector[key] = orig
		} else {
			a.OriginalSelector[key] = nil
		}
	}
	if _, exists := a.OriginalScheduler[key]; !exists {
		a.OriginalScheduler[key] = deployment.Spec.Template.Spec.SchedulerName
	}
	return nil
}

func (a *MigrateServiceAction) patchAndScaleDown(ctx context.Context, clientset *kubernetes.Clientset, namespace, target, targetNode, key string) error {
	deploymentsClient := clientset.AppsV1().Deployments(namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(ctx, target, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get deployment for migration: %w", getErr)
		}
		schedulerName := strings.TrimSpace(deployment.Spec.Template.Spec.SchedulerName)
		if schedulerName != "" && schedulerName != "default-scheduler" {
			return fmt.Errorf(
				"migration blocked for %s/%s: unsupported schedulerName %q (requires default scheduler to honor nodeSelector migration)",
				namespace,
				target,
				schedulerName,
			)
		}
		deployment.Spec.Template.Spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": targetNode,
		}
		zero := int32(0)
		deployment.Spec.Replicas = &zero
		_, updateErr := deploymentsClient.Update(ctx, deployment, metav1.UpdateOptions{})
		return updateErr
	})
}

func (a *MigrateServiceAction) waitForPodsTerminated(ctx context.Context, clientset *kubernetes.Clientset, namespace, target string) error {
	for i := 0; i < 30; i++ {
		pods, listErr := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app=%s", target),
		})
		if listErr != nil {
			return fmt.Errorf("failed to list pods while waiting for termination: %w", listErr)
		}
		if len(pods.Items) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for pods to terminate")
		case <-time.After(1 * time.Second):
		}
	}
	pods, listErr := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", target),
	})
	if listErr != nil {
		return fmt.Errorf("timed out waiting for pods to terminate and failed final pod listing: %w", listErr)
	}
	remaining := make([]string, 0, len(pods.Items))
	for _, pod := range pods.Items {
		remaining = append(remaining, pod.Name)
	}
	sort.Strings(remaining)
	return fmt.Errorf("timed out waiting for pods to terminate for %s/%s; remaining pods: %s", namespace, target, strings.Join(remaining, ", "))
}

func (a *MigrateServiceAction) scaleUpOnTarget(ctx context.Context, clientset *kubernetes.Clientset, namespace, target string, replicas int32, key string) error {
	desired := replicas
	if desired <= 0 {
		desired = a.OriginalReplicas[key]
	}
	deploymentsClient := clientset.AppsV1().Deployments(namespace)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(ctx, target, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get deployment for scale up: %w", getErr)
		}
		deployment.Spec.Replicas = &desired
		_, updateErr := deploymentsClient.Update(ctx, deployment, metav1.UpdateOptions{})
		return updateErr
	}); err != nil {
		return err
	}
	return a.waitForDeploymentReady(ctx, clientset, namespace, target, desired)
}

func (a *MigrateServiceAction) waitForDeploymentReady(ctx context.Context, clientset *kubernetes.Clientset, namespace, target string, desired int32) error {
	deadline := time.Now().Add(2 * time.Minute)
	deploymentsClient := clientset.AppsV1().Deployments(namespace)

	for {
		deployment, err := deploymentsClient.Get(ctx, target, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to fetch deployment status for %s/%s: %w", namespace, target, err)
		}

		observed := deployment.Status.ObservedGeneration >= deployment.Generation
		if desired == 0 {
			if observed && deployment.Status.Replicas == 0 && deployment.Status.ReadyReplicas == 0 {
				return nil
			}
		} else if observed &&
			deployment.Status.UpdatedReplicas >= desired &&
			deployment.Status.ReadyReplicas >= desired &&
			deployment.Status.AvailableReplicas >= desired {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf(
				"deployment %s/%s not ready after 2m (desired=%d observed=%d/%d updated=%d ready=%d available=%d)",
				namespace,
				target,
				desired,
				deployment.Status.ObservedGeneration,
				deployment.Generation,
				deployment.Status.UpdatedReplicas,
				deployment.Status.ReadyReplicas,
				deployment.Status.AvailableReplicas,
			)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled while waiting for deployment %s/%s readiness", namespace, target)
		case <-time.After(2 * time.Second):
		}
	}
}

func (a *MigrateServiceAction) Rollback(ctx context.Context, namespace, target string, config json.RawMessage) error {
	clientset, err := getK8sClient(a.clients)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s/%s", namespace, target)
	deploymentsClient := clientset.AppsV1().Deployments(namespace)
	origReplicas := int32(1)
	if r, exists := a.OriginalReplicas[key]; exists {
		origReplicas = r
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, getErr := deploymentsClient.Get(ctx, target, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get deployment for rollback: %w", getErr)
		}

		// Restore original nodeSelector
		if origSelector, exists := a.OriginalSelector[key]; exists {
			deployment.Spec.Template.Spec.NodeSelector = origSelector
		} else {
			deployment.Spec.Template.Spec.NodeSelector = nil
		}
		if schedulerName, exists := a.OriginalScheduler[key]; exists {
			deployment.Spec.Template.Spec.SchedulerName = schedulerName
		}

		// Restore original replicas
		deployment.Spec.Replicas = &origReplicas

		_, updateErr := deploymentsClient.Update(ctx, deployment, metav1.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}
	if err := a.waitForDeploymentReady(ctx, clientset, namespace, target, origReplicas); err != nil {
		return fmt.Errorf("rollback completed but deployment did not recover: %w", err)
	}

	delete(a.OriginalReplicas, key)
	delete(a.OriginalSelector, key)
	delete(a.OriginalScheduler, key)
	return nil
}

func findContainerIndex(deployment *appsv1.Deployment, preferredName string) (int, error) {
	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return 0, fmt.Errorf("deployment %s/%s has no containers", deployment.Namespace, deployment.Name)
	}
	if preferredName == "" {
		return 0, nil
	}
	for i, container := range containers {
		if container.Name == preferredName {
			return i, nil
		}
	}
	if len(containers) == 1 {
		return 0, nil
	}
	return 0, fmt.Errorf("container %q not found in deployment %s/%s", preferredName, deployment.Namespace, deployment.Name)
}

func getEnvVar(envs []corev1.EnvVar, name string) (string, bool) {
	for _, env := range envs {
		if env.Name == name {
			return env.Value, true
		}
	}
	return "", false
}

func setEnvVar(envs []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for i := range envs {
		if envs[i].Name == name {
			envs[i].Value = value
			envs[i].ValueFrom = nil
			return envs
		}
	}
	return append(envs, corev1.EnvVar{Name: name, Value: value})
}

func restoreEnvVar(envs []corev1.EnvVar, name string, snapshot envVarSnapshot) []corev1.EnvVar {
	if snapshot.Exists {
		return setEnvVar(envs, name, snapshot.Value)
	}
	filtered := envs[:0]
	for _, env := range envs {
		if env.Name != name {
			filtered = append(filtered, env)
		}
	}
	return filtered
}

func listNetworkPolicyNames(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]string, error) {
	list, err := clientset.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	sort.Strings(names)
	return names, nil
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func diffStringSets(expected, actual []string) (missing []string, extra []string) {
	expectedSet := make(map[string]struct{}, len(expected))
	actualSet := make(map[string]struct{}, len(actual))

	for _, v := range expected {
		expectedSet[v] = struct{}{}
	}
	for _, v := range actual {
		actualSet[v] = struct{}{}
	}
	for _, v := range expected {
		if _, ok := actualSet[v]; !ok {
			missing = append(missing, v)
		}
	}
	for _, v := range actual {
		if _, ok := expectedSet[v]; !ok {
			extra = append(extra, v)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func truncateStrings(items []string, limit int) []string {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	out := make([]string, 0, limit+1)
	out = append(out, items[:limit]...)
	out = append(out, fmt.Sprintf("...+%d more", len(items)-limit))
	return out
}
