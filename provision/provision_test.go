package provision

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"text/template"
	"time"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/containership/csctl/cloud"
	"github.com/containership/csctl/cloud/provision/types"
	"github.com/containership/csctl/resource/options"

	"github.com/containership/e2e-test/constants"
	"github.com/containership/e2e-test/util"
)

// The provisionContext is different from the context required for other tests,
// thus we split it out here
type provisionContext struct {
	containershipClientset cloud.Interface
	kubernetesClientset    kubernetes.Interface

	// this is only required because we can't pull the token back out of
	// the Containership clientset to use it again
	authToken string

	kubeconfigFilename string

	// These will be initialized at different times; however, once they are
	// set, they should never be mutated again.
	organizationID string
	templateID     string
	clusterID      string
}

var context *provisionContext

const (
	provisionPollInterval = 1 * time.Second
	provisionTimeout      = 30 * time.Minute
)

// Flags
var (
	templateFilename     string
	providerID           string
	kubernetesVersion    string
	sshPublicKeyFilename string
	sshPublicKeyBase64   string

	debugEnabled bool
)

func init() {
	flag.StringVar(&templateFilename, "template", "", "path to template file to use")
	flag.StringVar(&providerID, "provider", "", "provider ID to use")
	flag.StringVar(&kubernetesVersion, "kubernetes-version", "", "Kubernetes version to provision (without leading 'v')")
	flag.StringVar(&sshPublicKeyFilename, "ssh-public-key-file", "", "path to SSH public key file to provide in template if applicable (can't be combined with --ssh-public-key)")
	flag.StringVar(&sshPublicKeyBase64, "ssh-public-key", "", "Base64-encoded SSH public key to provide in template if applicable (can't be combined with --ssh-public-key-file)")

	flag.BoolVar(&debugEnabled, "debug", false, "enable Containership client debug mode (not to be confused with ginkgo debug, which is separate)")
}

func TestProvision(t *testing.T) {
	// Hook up gomega to ginkgo
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provision Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	// Run only on first node
	token := os.Getenv("CONTAINERSHIP_TOKEN")
	Expect(token).NotTo(BeEmpty(), "please specify a Containership Cloud token via CONTAINERSHIP_TOKEN env var")

	kubeconfigFilename := os.Getenv("KUBECONFIG")
	Expect(kubeconfigFilename).NotTo(BeEmpty(), "please set KUBECONFIG environment variable")

	// Check flags
	// TODO can we just use cobra somehow and simply mark flags as required?
	Expect(templateFilename).NotTo(BeEmpty(), "please specify template filename (full path) using --template")
	Expect(providerID).NotTo(BeEmpty(), "please specify provider ID using --provider")
	Expect(kubernetesVersion).NotTo(BeEmpty(), "please specify Kubernetes version using --kubernetes-version")

	clientset, err := cloud.New(cloud.Config{
		Token:            token,
		APIBaseURL:       constants.StageAPIBaseURL,
		AuthBaseURL:      constants.StageAuthBaseURL,
		ProvisionBaseURL: constants.StageProvisionBaseURL,
		DebugEnabled:     debugEnabled,
	})
	Expect(err).NotTo(HaveOccurred())

	context = &provisionContext{
		containershipClientset: clientset,
		authToken:              token,
		kubeconfigFilename:     kubeconfigFilename,
		organizationID:         constants.TestOrganizationID,
	}

	return nil
}, func(_ []byte) {
	// Run on all nodes after first one
})

var _ = Describe("Provisioning a cluster", func() {
	It("should successfully create the template", func() {
		By("creating template from file")
		tmpl, err := template.ParseFiles(templateFilename)
		Expect(err).NotTo(HaveOccurred())

		if sshPublicKeyFilename != "" && sshPublicKeyBase64 != "" {
			Fail("please specify one or neither of --ssh-public-key-file or --ssh-public-key, but not both")
		}

		var sshPublicKey string
		if sshPublicKeyFilename != "" {
			By("reading SSH public key from file")
			b, err := ioutil.ReadFile(sshPublicKeyFilename)
			Expect(err).NotTo(HaveOccurred())
			sshPublicKey = string(b)
		} else if sshPublicKeyBase64 != "" {
			By("reading base64-decoding the SSH public key")
			b, err := base64.StdEncoding.DecodeString(sshPublicKeyBase64)
			Expect(err).NotTo(HaveOccurred())
			sshPublicKey = string(b)
		}

		By("executing template")
		// Assuming same version, OS across all pools for now
		var buf bytes.Buffer
		values := struct {
			MasterKubernetesVersion string
			WorkerKubernetesVersion string
			Description             string
			Timestamp               string
			SSHPublicKey            string
		}{
			MasterKubernetesVersion: kubernetesVersion,
			WorkerKubernetesVersion: kubernetesVersion,
			Description:             fmt.Sprintf("e2e-%s", kubernetesVersion),
			Timestamp:               timestamp(),
			SSHPublicKey:            sshPublicKey,
		}
		Expect(tmpl.Execute(&buf, values)).To(Succeed())

		By("reading bytes from executed template buffer")
		bytes, err := ioutil.ReadAll(&buf)
		Expect(err).NotTo(HaveOccurred())

		By("unmarshalling template into request struct")
		req := &types.CreateTemplateRequest{}
		Expect(yaml.Unmarshal(bytes, req)).To(Succeed())

		operatingSystem := ""
		for _, v := range req.Configuration.Variable {
			// Since we're assuming the same OS across all node pools, any
			// will do - grab the first
			Expect(v.Default.Os).NotTo(BeNil())
			operatingSystem = *v.Default.Os
			break
		}
		Expect(operatingSystem).NotTo(BeEmpty())

		// Super gross but we only have the OS from the executed template
		// currently, so just tack it on the already-existing description
		desc := fmt.Sprintf("%s-%s", *req.Description, operatingSystem)
		req.Description = &desc

		By("POSTing the template create request")
		resp, err := context.containershipClientset.Provision().
			Templates(context.organizationID).
			Create(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())

		// Set template ID in global context - should never be mutated after this
		context.templateID = string(resp.ID)
	})

	It("should successfully initiate provisioning", func() {
		By("getting the template")
		template, err := context.containershipClientset.Provision().
			Templates(context.organizationID).
			Get(context.templateID)
		Expect(err).NotTo(HaveOccurred())

		By("building a cluster create request")
		var req types.CreateCKEClusterRequest
		genericOptions := options.ClusterCreate{
			ProviderID:  providerID,
			TemplateID:  context.templateID,
			Name:        *template.Description,
			Environment: "e2e-test",
		}

		switch *template.ProviderName {
		case "amazon_web_services":
			opts := &options.AWSClusterCreate{
				ClusterCreate: genericOptions,
			}
			Expect(opts.DefaultAndValidate()).To(Succeed())
			req = opts.CreateCKEClusterRequest()

		case "azure":
			opts := &options.AzureClusterCreate{
				ClusterCreate: genericOptions,
			}
			Expect(opts.DefaultAndValidate()).To(Succeed())
			req = opts.CreateCKEClusterRequest()

		case "digital_ocean":
			opts := &options.DigitalOceanClusterCreate{
				ClusterCreate: genericOptions,
			}
			Expect(opts.DefaultAndValidate()).To(Succeed())
			req = opts.CreateCKEClusterRequest()

		case "google":
			opts := &options.GoogleClusterCreate{
				ClusterCreate: genericOptions,
			}
			Expect(opts.DefaultAndValidate()).To(Succeed())
			req = opts.CreateCKEClusterRequest()

		case "packet":
			opts := &options.PacketClusterCreate{
				ClusterCreate: genericOptions,
			}
			Expect(opts.DefaultAndValidate()).To(Succeed())
			req = opts.CreateCKEClusterRequest()

		default:
			// Should never happen
			Fail(fmt.Sprintf("unknown provider name %q", *template.ProviderName))
		}

		By("POSTing the cluster create request")
		resp, err := context.containershipClientset.Provision().
			CKEClusters(context.organizationID).
			Create(&req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())

		// Set cluster ID in global context - should never be mutated after this
		context.clusterID = string(resp.ID)
	})

	It("should successfully write kubeconfig", func() {
		Expect(writeKubeconfig(context.kubeconfigFilename,
			context.organizationID,
			context.clusterID,
			context.authToken)).
			Should(Succeed())
	})

	It("should successfully initialize a Kubernetes clientset", func() {
		cfg, err := clientcmd.BuildConfigFromFlags("", context.kubeconfigFilename)
		Expect(err).NotTo(HaveOccurred())

		kubeClientset, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		// Set Kubernetes clientset in global context - should never be mutated after this
		context.kubernetesClientset = kubeClientset
	})

	It("should eventually attach properly (report as running)", func() {
		Expect(waitForClusterRunning()).Should(Succeed())
	})

	It("should eventually have all node pools report as running", func() {
		Expect(waitForAllNodePoolsRunning()).Should(Succeed())
	})

	It("should eventually have a reachable API server", func() {
		Expect(waitForKubernetesAPIReady()).Should(Succeed())
	})

	It("should have all nodes ready in Kubernetes API", func() {
		Expect(util.WaitForKubernetesNodesReady(context.kubernetesClientset)).Should(Succeed())
	})
})

func waitForClusterRunning() error {
	return wait.PollImmediate(provisionPollInterval, provisionTimeout, func() (bool, error) {
		cluster, err := context.containershipClientset.Provision().
			CKEClusters(context.organizationID).
			Get(context.clusterID)
		if err != nil {
			return false, errors.Wrap(err, "GETing cluster")
		}

		status := *cluster.Status.Type
		switch status {
		case "RUNNING":
			return true, nil
		case "PROVISIONING":
			return false, nil
		default:
			return false, errors.Errorf("cluster entered unexpected state %q", status)
		}
	})
}

func waitForAllNodePoolsRunning() error {
	return wait.PollImmediate(constants.DefaultPollInterval,
		constants.DefaultTimeout,
		func() (bool, error) {
			pools, err := context.containershipClientset.Provision().
				NodePools(context.organizationID, context.clusterID).
				List()
			if err != nil {
				return false, errors.Wrap(err, "GETing node pools")
			}

			running := true
			for _, pool := range pools {
				status := *pool.Status.Type
				switch status {
				case "RUNNING":
					continue
				case "UPDATING":
					running = false
				default:
					return false, errors.Errorf("node pool %q entered unexpected state %q", pool.ID, status)
				}
			}

			return running, nil
		})
}

func waitForKubernetesAPIReady() error {
	return wait.PollImmediate(constants.DefaultPollInterval,
		constants.DefaultTimeout,
		func() (bool, error) {
			_, err := context.kubernetesClientset.CoreV1().
				Pods(corev1.NamespaceDefault).
				List(metav1.ListOptions{})
			if err != nil {
				// Ignore auth errors because we're aggressively polling
				// the cluster before the roles and bindings may be synced
				if util.IsRetryableAPIError(err) || util.IsAuthError(err) {
					return false, nil
				}

				return false, errors.Wrap(err, "listing pods in default namespace to check API health")
			}

			return true, nil
		})
}

func timestamp() string {
	t := time.Now().UTC()
	return t.Format("20060102150405")
}

func writeKubeconfig(filename, organizationID, clusterID, authToken string) error {
	const kubeconfigTemplate = `
apiVersion: v1
clusters:
- cluster:
    server: https://stage-proxy.containership.io/v3/organizations/{{.OrganizationID}}/clusters/{{.ClusterID}}/k8sapi/proxy
  name: cs-e2e-test-cluster
contexts:
- context:
    cluster: cs-e2e-test-cluster
    user: cs-e2e-test-user
  name: cs-e2e-test-ctx
current-context: cs-e2e-test-ctx
kind: Config
preferences: {}
users:
- name: cs-e2e-test-user
  user:
    token: {{.AuthToken}}
`

	tmpl := template.Must(template.New("kubeconfig").Parse(kubeconfigTemplate))

	f, err := os.Create(filename)
	if err != nil {
		return err
	}

	values := struct {
		OrganizationID string
		ClusterID      string
		AuthToken      string
	}{
		OrganizationID: organizationID,
		ClusterID:      clusterID,
		AuthToken:      authToken,
	}

	return tmpl.Execute(f, values)
}
