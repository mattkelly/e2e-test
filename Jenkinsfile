@Library('containership-jenkins@v3')
import io.containership.*
import java.text.SimpleDateFormat

def dockerUtils = new Docker(this)
def gitUtils = new Git(this)
def pipelineUtils = new Pipeline(this)

def csTokenId = "containershipbot_cs_token"
def testImage = "containership/e2e-test:latest"

def slackNotifierTokenId = 'slack_notifier_token_cs';
def slack_color = '#228B22'

def supportedKubernetesVersions = ['1.13.10', '1.14.6', '1.15.3']

// By default, run all supported versions
def runVersions = supportedKubernetesVersions
if (env.KUBERNETES_VERSION) {
    // A specific version was specified, so only run that version.
    // Don't verify that the version is valid/supported - assume
    // that the caller knows what they're doing.
    runVersions = [env.KUBERNETES_VERSION]
}

def slack_message = """
Job: ${env.JOB_NAME}
Build: ${env.BUILD_NUMBER}
Provider: ${env.PROVIDER_NAME}
Template: ${env.TEMPLATE_NAME}
Versions: ${runVersions}
URL: ${env.RUN_DISPLAY_URL}
""";

def providerIDs = [
"amazon_web_services": "47b7617e-3ed5-44e3-ae46-573eb23d674b",
"azure": "f1d81322-5ab5-42be-9e44-a6a7c99c5ca2",
"digital_ocean": "08cd67a1-6837-487d-894d-d01827fbf840",
"google": "32c22c2c-f99d-4db6-8923-2ed98dedf01d",
"packet": "62af3ddc-bc36-4272-a7bf-29fb61babd24",
]

try {
    pipelineUtils.jenkinsWithNodeTemplate {
        properties(
            [
            pipelineTriggers(
                [
                parameterizedCron('''
                # -- AWS --
                H 10 * * * % PROVIDER_NAME=amazon_web_services; TEMPLATE_NAME=centos
                H 12 * * * % PROVIDER_NAME=amazon_web_services; TEMPLATE_NAME=ubuntu

                # -- Azure --
                H 10 * * * % PROVIDER_NAME=azure; TEMPLATE_NAME=centos
                H 12 * * * % PROVIDER_NAME=azure; TEMPLATE_NAME=ubuntu

                # -- DigitalOcean --
                H 10 * * * % PROVIDER_NAME=digital_ocean; TEMPLATE_NAME=centos
                H 12 * * * % PROVIDER_NAME=digital_ocean; TEMPLATE_NAME=ubuntu

                # -- Google --
                H 10 * * * % PROVIDER_NAME=google; TEMPLATE_NAME=centos
                H 12 * * * % PROVIDER_NAME=google; TEMPLATE_NAME=ubuntu

                # -- Packet --
                # Runs must be staggered in order to not overload this provider
                # TODO no real evidence to support this besides consistently
                # failing runs when not staggered due to instances failing to come up
                # TODO it'd be neat if we could just index into
                # supportedKubernetesVersions here to make it less fragile
                # CentOS
                H 10 * * * % PROVIDER_NAME=packet; TEMPLATE_NAME=centos; KUBERNETES_VERSION=1.13.10
                H 11 * * * % PROVIDER_NAME=packet; TEMPLATE_NAME=centos; KUBERNETES_VERSION=1.14.6
                H 12 * * * % PROVIDER_NAME=packet; TEMPLATE_NAME=centos; KUBERNETES_VERSION=1.15.3

                # Ubuntu
                H 13 * * * % PROVIDER_NAME=packet; TEMPLATE_NAME=ubuntu; KUBERNETES_VERSION=1.13.10
                H 14 * * * % PROVIDER_NAME=packet; TEMPLATE_NAME=ubuntu; KUBERNETES_VERSION=1.14.6
                H 15 * * * % PROVIDER_NAME=packet; TEMPLATE_NAME=ubuntu; KUBERNETES_VERSION=1.15.3
                '''
                )
                ]
            )
            ]
        )

        // deploy to all the clusters
        withCredentials([string(credentialsId: csTokenId, variable: 'CS_TOKEN')]) {
            stage('Prepare Testing') {
                container('docker') {
                    dockerUtils.login(pipelineUtils.getDockerCredentialId())
                    dockerUtils.pullImage(testImage)
                }
            }

            def testEnvVars = [
            "CONTAINERSHIP_TOKEN=${env.CS_TOKEN}",
            "KUBECONFIG=kube.conf"
            ]

            def providerID = providerIDs[env.PROVIDER_NAME]
            if (!providerID) {
                throw new Exception("Unknown provider name: ${env.PROVIDER_NAME}")
            }

            def provisionBaseArgs = [
            "--template resources/templates/${env.PROVIDER_NAME}/${env.TEMPLATE_NAME}.yaml",
            "--provider ${providerID}",
            ]

            if (env.SSH_PUBLIC_KEY) {
                provisionBaseArgs.add("--ssh-public-key ${env.SSH_PUBLIC_KEY}")
            }

            // Run all supported Kubernetes versions in parallel
            // `parallel` expects a map, so build it here
            def parallelStageMap = runVersions.collectEntries { version ->
                [
                "${version}" : {
                    stage("Kubernetes ${version} Test") {
                        def provisionArgs = provisionBaseArgs.clone()
                        provisionArgs.add("--kubernetes-version ${version}")

                        container('docker') {
                            dockerUtils.runCommand(testImage,
                            "./scripts/provision-and-test.sh",
                            provisionArgs.join(" "),
                            testEnvVars)
                        }
                    }
                }
                ]
            }

            parallel(parallelStageMap)
        }
    }

    slack_message = """
✅ Success
${slack_message}
""";
} catch(error) {
    def sw = new StringWriter();
    def pw = new PrintWriter(sw);
    error.printStackTrace(pw);
    echo sw.toString();
    def err_str = error.toString();

    currentBuild.result = 'FAILURE';

    slack_color = '#B22222';
    slack_message = """
🚨 Failure

${slack_message}
(Notifying @channel)

Error:
```
${err_str}
```
""";
} finally {
    slackSend channel: "#e2e-tests", color: "${slack_color}", message: "${slack_message}";
}
