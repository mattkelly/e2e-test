package scale

import (
	"os"
	"testing"

	"github.com/pkg/errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/containership/csctl/cloud"
	"github.com/containership/csctl/cloud/provision/types"

	"github.com/containership/e2e-test/constants"
	testcontext "github.com/containership/e2e-test/tests/context"
	"github.com/containership/e2e-test/util"
)

type scaleContext struct {
	*testcontext.E2eTest

	// Node pool ID of the pool we're currently operating on.
	// Required to operate on the same pool across multiple It blocks (in order
	// to ideally end up back at the same state - i.e. scale a pool up and then
	// scale it back down)
	currentNodePoolID string
}

var context *scaleContext

func TestScale(t *testing.T) {
	// Hook up gomega to ginkgo
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scale Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	// Run only on first node
	token := os.Getenv("CONTAINERSHIP_TOKEN")
	Expect(token).NotTo(BeEmpty(), "please specify a Containership Cloud token via CONTAINERSHIP_TOKEN env var")

	kubeconfigFilename := os.Getenv("KUBECONFIG")
	Expect(kubeconfigFilename).NotTo(BeEmpty(), "please set KUBECONFIG environment variable")

	clientset, err := cloud.New(cloud.Config{
		Token:            token,
		APIBaseURL:       constants.StageAPIBaseURL,
		AuthBaseURL:      constants.StageAuthBaseURL,
		ProvisionBaseURL: constants.StageProvisionBaseURL,
	})
	Expect(err).NotTo(HaveOccurred())

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigFilename)
	Expect(err).NotTo(HaveOccurred())

	kubeClientset, err := kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	clusterID, err := util.GetClusterIDFromKubernetes(kubeClientset)
	Expect(err).NotTo(HaveOccurred())

	context = &scaleContext{
		E2eTest: &testcontext.E2eTest{
			ContainershipClientset: clientset,
			KubernetesClientset:    kubeClientset,
			OrganizationID:         constants.TestOrganizationID,
			ClusterID:              clusterID,
		},
	}

	return nil
}, func(_ []byte) {
	// Run on all nodes after first one
})

var _ = Describe("Scaling a worker node pool", func() {
	It("should successfully request to scale up by one", func() {
		By("listing node pools")
		nodePools, err := context.ContainershipClientset.Provision().
			NodePools(context.OrganizationID, context.ClusterID).
			List()
		Expect(err).NotTo(HaveOccurred())

		// Any worker pool will do
		var pool *types.NodePool
		for _, p := range nodePools {
			if *p.KubernetesMode == "worker" {
				pool = &p
				break
			}
		}
		// TODO should we just skip all of these tests if no worker pool is found?
		// For now, let's assume there is a worker pool.
		Expect(pool).NotTo(BeNil())

		// Save the pool that we're operating on in the context
		context.currentNodePoolID = string(pool.ID)

		targetCount := *pool.Count + 1
		req := types.NodePoolScaleRequest{
			Count: &targetCount,
		}

		By("sending request to provision API")
		pool, err = context.ContainershipClientset.Provision().
			NodePools(context.OrganizationID, context.ClusterID).
			Scale(string(pool.ID), &req)
		Expect(err).NotTo(HaveOccurred())
		Expect(*pool.Count).To(Equal(targetCount))
	})

	It("should go into UPDATING state", func() {
		Expect(context.waitForNodePoolUpdating(context.currentNodePoolID)).Should(Succeed())
	})

	It("should return to RUNNING state", func() {
		Expect(context.waitForNodePoolRunning(context.currentNodePoolID)).Should(Succeed())
	})

	It("should have all Kubernetes nodes ready", func() {
		By("verifying that updated number of Kubernetes nodes is correct")
		pool, err := context.getCurrentPool()
		Expect(err).NotTo(HaveOccurred())

		label := "containership.io/node-pool-id=" + context.currentNodePoolID
		nodeList, err := context.KubernetesClientset.CoreV1().
			Nodes().
			List(metav1.ListOptions{
				LabelSelector: label,
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodeList.Items)).To(Equal(int(*pool.Count)))

		Expect(util.WaitForKubernetesNodesReady(context.KubernetesClientset)).Should(Succeed())
	})

	It("should successfully request to scale down by one", func() {
		By("getting the current pool")
		pool, err := context.getCurrentPool()
		Expect(err).NotTo(HaveOccurred())

		targetCount := *pool.Count - 1
		req := types.NodePoolScaleRequest{
			Count: &targetCount,
		}

		By("sending request to provision API")
		pool, err = context.ContainershipClientset.Provision().
			NodePools(context.OrganizationID, context.ClusterID).
			Scale(string(pool.ID), &req)
		Expect(err).NotTo(HaveOccurred())
		Expect(*pool.Count).To(Equal(targetCount))
	})

	// Note that we don't check for a transition to UPDATING here because a
	// scale down operation (delete) can happen so quickly that the transition
	// is missed.

	It("should return to RUNNING state", func() {
		Expect(context.waitForNodePoolRunning(context.currentNodePoolID)).Should(Succeed())
	})

	It("should have all Kubernetes nodes ready", func() {
		By("verifying that updated number of Kubernetes nodes is correct")
		pool, err := context.getCurrentPool()
		Expect(err).NotTo(HaveOccurred())

		label := "containership.io/node-pool-id=" + context.currentNodePoolID
		nodeList, err := context.KubernetesClientset.CoreV1().
			Nodes().
			List(metav1.ListOptions{
				LabelSelector: label,
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodeList.Items)).To(Equal(int(*pool.Count)))

		Expect(util.WaitForKubernetesNodesReady(context.KubernetesClientset)).Should(Succeed())
	})
})

func (c *scaleContext) getCurrentPool() (*types.NodePool, error) {
	return c.ContainershipClientset.Provision().
		NodePools(c.OrganizationID, c.ClusterID).
		Get(c.currentNodePoolID)
}

func (c *scaleContext) waitForNodePoolUpdating(id string) error {
	return wait.PollImmediate(constants.DefaultPollInterval,
		constants.DefaultTimeout,
		func() (bool, error) {
			pool, err := c.ContainershipClientset.Provision().
				NodePools(c.OrganizationID, c.ClusterID).
				Get(id)
			if err != nil {
				return false, errors.Wrapf(err, "GETing node pool %q", id)
			}

			status := *pool.Status.Type
			switch status {
			case "RUNNING":
				return false, nil
			case "UPDATING":
				return true, nil
			default:
				return false, errors.Errorf("node pool %q entered unexpected state %q", pool.ID, status)
			}
		})
}

func (c *scaleContext) waitForNodePoolRunning(id string) error {
	return wait.PollImmediate(constants.DefaultPollInterval,
		constants.DefaultTimeout,
		func() (bool, error) {
			pool, err := c.ContainershipClientset.Provision().
				NodePools(c.OrganizationID, c.ClusterID).
				Get(id)
			if err != nil {
				return false, errors.Wrapf(err, "GETing node pool %q", id)
			}

			status := *pool.Status.Type
			switch status {
			case "UPDATING":
				return false, nil
			case "RUNNING":
				return true, nil
			default:
				return false, errors.Errorf("node pool %q entered unexpected state %q", pool.ID, status)
			}
		})
}
