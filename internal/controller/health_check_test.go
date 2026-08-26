package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"fmt"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// mockPodExecuter implements PodExecuter for testing.
type mockPodExecuter struct {
	responses map[string]string // podName -> stdout
	errors    map[string]error  // podName -> error
}

func (m *mockPodExecuter) Exec(ctx context.Context, namespace, podName string, command []string) (string, error) {
	if err, ok := m.errors[podName]; ok {
		return "", err
	}
	if resp, ok := m.responses[podName]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("pod %s not found in mock", podName)
}

var _ = Describe("ZkServiceHealthCheck", func() {
	var (
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(zkv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
	})

	Describe("CheckHealthy", func() {
		Context("standalone mode (1 replica)", func() {
			It("should return healthy when node reports standalone", func() {
				cr := makeZookeeperCluster(1)
				pod := makePod("test-zk-server-default-0")

				k8sClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(cr, pod).
					Build()

				executer := &mockPodExecuter{
					responses: map[string]string{
						"test-zk-server-default-0": "Mode: standalone\n",
					},
				}

				checker := NewZkServiceHealthCheck(executer)
				healthy, err := checker.CheckHealthy(context.Background(), k8sClient, "default", "test-zk")
				Expect(err).NotTo(HaveOccurred())
				Expect(healthy).To(BeTrue())
			})

			It("should return unhealthy when node is non-responsive", func() {
				cr := makeZookeeperCluster(1)
				pod := makePod("test-zk-server-default-0")

				k8sClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(cr, pod).
					Build()

				executer := &mockPodExecuter{
					errors: map[string]error{
						"test-zk-server-default-0": fmt.Errorf("connection refused"),
					},
				}

				checker := NewZkServiceHealthCheck(executer)
				healthy, err := checker.CheckHealthy(context.Background(), k8sClient, "default", "test-zk")
				Expect(err).NotTo(HaveOccurred())
				Expect(healthy).To(BeFalse())
			})
		})

		Context("ensemble mode (3 replicas)", func() {
			It("should return healthy with 1 leader and 2 followers", func() {
				cr := makeZookeeperCluster(3)
				objs := []runtime.Object{
					makePod("test-zk-server-default-0"),
					makePod("test-zk-server-default-1"),
					makePod("test-zk-server-default-2"),
					cr,
				}

				k8sClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(objs...).
					Build()

				executer := &mockPodExecuter{
					responses: map[string]string{
						"test-zk-server-default-0": "Mode: leader\n",
						"test-zk-server-default-1": "Mode: follower\n",
						"test-zk-server-default-2": "Mode: follower\n",
					},
				}

				checker := NewZkServiceHealthCheck(executer)
				healthy, err := checker.CheckHealthy(context.Background(), k8sClient, "default", "test-zk")
				Expect(err).NotTo(HaveOccurred())
				Expect(healthy).To(BeTrue())
			})

			It("should return unhealthy when quorum lost (majority down)", func() {
				cr := makeZookeeperCluster(3)
				objs := []runtime.Object{
					makePod("test-zk-server-default-0"),
					makePod("test-zk-server-default-1"),
					makePod("test-zk-server-default-2"),
					cr,
				}

				k8sClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(objs...).
					Build()

				executer := &mockPodExecuter{
					responses: map[string]string{
						"test-zk-server-default-0": "Mode: leader\n",
					},
					errors: map[string]error{
						"test-zk-server-default-1": fmt.Errorf("connection refused"),
						"test-zk-server-default-2": fmt.Errorf("connection refused"),
					},
				}

				checker := NewZkServiceHealthCheck(executer)
				healthy, err := checker.CheckHealthy(context.Background(), k8sClient, "default", "test-zk")
				Expect(err).NotTo(HaveOccurred())
				Expect(healthy).To(BeFalse())
			})

			It("should return unhealthy when no leader (split brain)", func() {
				cr := makeZookeeperCluster(3)
				objs := []runtime.Object{
					makePod("test-zk-server-default-0"),
					makePod("test-zk-server-default-1"),
					makePod("test-zk-server-default-2"),
					cr,
				}

				k8sClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(objs...).
					Build()

				executer := &mockPodExecuter{
					responses: map[string]string{
						"test-zk-server-default-0": "Mode: follower\n",
						"test-zk-server-default-1": "Mode: follower\n",
						"test-zk-server-default-2": "Mode: follower\n",
					},
				}

				checker := NewZkServiceHealthCheck(executer)
				healthy, err := checker.CheckHealthy(context.Background(), k8sClient, "default", "test-zk")
				Expect(err).NotTo(HaveOccurred())
				Expect(healthy).To(BeFalse())
			})
		})

		Context("error handling", func() {
			It("should return error when CR not found", func() {
				k8sClient := fake.NewClientBuilder().
					WithScheme(scheme).
					Build()

				executer := &mockPodExecuter{}
				checker := NewZkServiceHealthCheck(executer)
				_, err := checker.CheckHealthy(context.Background(), k8sClient, "default", "nonexistent")
				Expect(err).To(HaveOccurred())
			})

			It("should return unhealthy when no pods found", func() {
				cr := makeZookeeperCluster(3)

				k8sClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(cr).
					Build()

				executer := &mockPodExecuter{}
				checker := NewZkServiceHealthCheck(executer)
				healthy, err := checker.CheckHealthy(context.Background(), k8sClient, "default", "test-zk")
				Expect(err).NotTo(HaveOccurred())
				Expect(healthy).To(BeFalse())
			})
		})
	})

	Describe("parseMode", func() {
		It("should parse leader mode", func() {
			Expect(parseMode("Mode: leader\n")).To(Equal("leader"))
		})

		It("should parse follower mode", func() {
			Expect(parseMode("Mode: follower\n")).To(Equal("follower"))
		})

		It("should parse standalone mode", func() {
			Expect(parseMode("Mode: standalone\n")).To(Equal("standalone"))
		})

		It("should return empty for unknown mode", func() {
			Expect(parseMode("Mode: unknown\n")).To(Equal(""))
		})

		It("should return empty for no mode line", func() {
			Expect(parseMode("Latency min/avg/max: 0/0/0\n")).To(Equal(""))
		})
	})
})

func makeZookeeperCluster(replicas int32) *zkv1alpha1.ZookeeperCluster {
	return &zkv1alpha1.ZookeeperCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-zk",
			Namespace: "default",
		},
		Spec: zkv1alpha1.ZookeeperClusterSpec{
			ClusterConfig: &zkv1alpha1.ClusterConfigSpec{},
			Servers: &zkv1alpha1.ServerSpec{
				RoleGroups: map[string]zkv1alpha1.RoleGroupSpec{
					"default": {
						Replicas: replicas,
					},
				},
			},
		},
	}
}

func makePod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"zookeeper.kubedoop.dev/cluster": "test-zk",
				"zookeeper.kubedoop.dev/role":    "server",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}
