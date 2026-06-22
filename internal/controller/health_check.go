package controller

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/zncdatadev/operator-go/pkg/common"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/security"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodExecuter abstracts pod command execution for testability.
type PodExecuter interface {
	Exec(ctx context.Context, namespace, podName string, command []string) (string, error)
}

// PodExec implements PodExecuter using client-go remotecommand.
type PodExec struct {
	clientset  *kubernetes.Clientset
	restConfig *rest.Config
}

// NewPodExec creates a PodExec from a rest.Config.
func NewPodExec(restConfig *rest.Config) (*PodExec, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return &PodExec{clientset: clientset, restConfig: restConfig}, nil
}

// Exec runs a command inside a pod and returns the combined stdout output.
func (e *PodExec) Exec(ctx context.Context, namespace, podName string, command []string) (string, error) {
	req := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   command,
			Container: "server",
			Stdin:     false,
			Stdout:    true,
			Stderr:    false,
		}, metav1.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(e.restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: nil,
		Tty:    false,
	})
	if err != nil {
		return "", fmt.Errorf("exec failed for pod %s: %w", podName, err)
	}

	return stdout.String(), nil
}

// ZkServiceHealthCheck implements common.ServiceHealthCheck for ZooKeeper ensemble health.
type ZkServiceHealthCheck struct {
	executer PodExecuter
}

// NewZkServiceHealthCheck creates a ZkServiceHealthCheck with the given PodExecuter.
func NewZkServiceHealthCheck(executer PodExecuter) *ZkServiceHealthCheck {
	return &ZkServiceHealthCheck{executer: executer}
}

// Compile-time interface check.
var _ common.ServiceHealthCheck = &ZkServiceHealthCheck{}

// CheckHealthy verifies ZooKeeper ensemble health by exec-ing into server pods
// and checking quorum via the srvr command.
func (z *ZkServiceHealthCheck) CheckHealthy(
	ctx context.Context,
	k8sClient client.Client,
	namespace, name string,
) (bool, error) {
	// Fetch the ZookeeperCluster CR
	cr := &zkv1alpha1.ZookeeperCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cr); err != nil {
		return false, fmt.Errorf("failed to get ZookeeperCluster %s/%s: %w", namespace, name, err)
	}

	// Resolve security config to determine client port
	zkSecurity, err := security.NewZookeeperSecurity(ctx, k8sClient, cr.Spec.ClusterConfig)
	if err != nil {
		return false, fmt.Errorf("failed to resolve security config: %w", err)
	}
	clientPort := zkSecurity.ClientPort()

	// List server pods for this cluster
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(map[string]string{
		reconciler.ClusterLabelKey(LabelDomain): name,
		reconciler.RoleLabelKey(LabelDomain):    "server",
	})
	if err := k8sClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return false, nil
	}

	// Determine expected replicas from the first role group
	expectedReplicas := getExpectedReplicas(cr)

	// Probe each pod
	type nodeStatus struct {
		mode    string // leader, follower, standalone
		healthy bool
	}
	nodes := make([]nodeStatus, 0, len(podList.Items))

	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		cmd := []string{
			"bash", "-c",
			fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d && echo srvr >&3 && grep '^Mode:' <&3", clientPort),
		}
		output, err := z.executer.Exec(ctx, namespace, pod.Name, cmd)
		if err != nil {
			continue // non-responsive node
		}
		mode := parseMode(output)
		if mode != "" {
			nodes = append(nodes, nodeStatus{mode: mode, healthy: true})
		}
	}

	// Quorum verification
	if expectedReplicas <= 1 {
		// Standalone: at least 1 healthy node with Mode: standalone
		for _, n := range nodes {
			if n.mode == "standalone" {
				return true, nil
			}
		}
		return false, nil
	}

	// Ensemble: exactly 1 leader AND majority responsive
	leaders := 0
	for _, n := range nodes {
		if n.mode == "leader" {
			leaders++
		}
	}
	if leaders != 1 {
		return false, nil
	}
	majority := expectedReplicas/2 + 1
	if len(nodes) >= majority {
		return true, nil
	}
	return false, nil
}

// parseMode extracts the Mode value from srvr command output.
// Expected output line: "Mode: follower" or "Mode: leader" or "Mode: standalone"
func parseMode(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Mode:") {
			mode := strings.TrimSpace(strings.TrimPrefix(line, "Mode:"))
			switch mode {
			case "leader", "follower", "standalone":
				return mode
			}
		}
	}
	return ""
}

// getExpectedReplicas returns the expected number of server replicas from the CR.
func getExpectedReplicas(cr *zkv1alpha1.ZookeeperCluster) int {
	if cr.Spec.Servers == nil || cr.Spec.Servers.RoleGroups == nil {
		return 1
	}
	total := 0
	for _, rg := range cr.Spec.Servers.RoleGroups {
		replicas := rg.Replicas
		if replicas > 0 {
			total += int(replicas)
		} else {
			total++
		}
	}
	if total == 0 {
		return 1
	}
	return total
}
