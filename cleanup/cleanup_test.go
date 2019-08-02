package cleanup

import (
	"os"
	"testing"
	"time"

	"github.com/pkg/errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/containership/csctl/cloud"
	"github.com/containership/csctl/cloud/rest"

	"github.com/containership/e2e-test/constants"
	testcontext "github.com/containership/e2e-test/tests/context"
	"github.com/containership/e2e-test/util"
)

type cleanupContext struct {
	*testcontext.E2eTest
}

var context *cleanupContext

const (
	deletePollInterval = 1 * time.Second
	deleteTimeout      = 8 * time.Minute
)

func TestScale(t *testing.T) {
	// Hook up gomega to ginkgo
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cleanup Suite")
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

	context = &cleanupContext{
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

var _ = Describe("Cleaning up a cluster", func() {
	It("should successfully request DELETE", func() {
		Expect(context.ContainershipClientset.Provision().
			CKEClusters(context.OrganizationID).
			Delete(context.ClusterID)).To(Succeed())
	})

	It("should successfully delete the cluster in cloud", func() {
		Expect(context.waitForClusterDeleted()).Should(Succeed())
	})
})

func (c *cleanupContext) waitForClusterDeleted() error {
	return wait.PollImmediate(deletePollInterval,
		deleteTimeout,
		func() (bool, error) {
			cluster, err := c.ContainershipClientset.Provision().
				CKEClusters(c.OrganizationID).
				Get(c.ClusterID)
			if err != nil {
				switch err := err.(type) {
				case rest.HTTPError:
					if err.IsNotFound() {
						return true, nil
					}
				}

				return false, errors.Wrap(err, "GETing cluster")
			}

			// Short-circuit if the cluster went into an unexpected state
			status := *cluster.Status.Type
			switch status {
			case "RUNNING", "DELETING":
				break
			default:
				return false, errors.Errorf("cluster %q entered unexpected state %q", c.ClusterID, status)
			}

			return false, nil
		})
}
