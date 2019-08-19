@Library('containership-jenkins@v3')
import io.containership.*
import java.text.SimpleDateFormat

def dockerUtils = new Docker(this)
def gitUtils = new Git(this)
def pipelineUtils = new Pipeline(this)

def csTokenId = "containershipbot_cs_token"
def testImage = "containership/e2e-test:latest"

def slackNotifierTokenId = 'slack_notifier_token_cs';
def slack_message = "${env.JOB_NAME} - build:${env.BUILD_NUMBER} (${env.RUN_DISPLAY_URL}) ";
def slack_color = '#228B22'

//def supportedKubernetesVersions = ['1.13.4', '1.14.3', '1.15.2']
def supportedKubernetesVersions = ['1.13.4', '1.14.3']

def providerIDs = [
"amazon_web_services": "47b7617e-3ed5-44e3-ae46-573eb23d674b",
"azure": "f1d81322-5ab5-42be-9e44-a6a7c99c5ca2",
"digital_ocean": "08cd67a1-6837-487d-894d-d01827fbf840",
"google": "32c22c2c-f99d-4db6-8923-2ed98dedf01d",
"packet": "62af3ddc-bc36-4272-a7bf-29fb61babd24",
]

try {
    pipelineUtils.jenkinsWithNodeTemplate {
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
            def parallelStageMap = supportedKubernetesVersions.collectEntries { version ->
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

    slack_message = "Success: ${slack_message}";
} catch(error) {
    def sw = new StringWriter();
    def pw = new PrintWriter(sw);
    error.printStackTrace(pw);
    echo sw.toString();
    def err_str = error.toString();

    currentBuild.result = 'FAILURE';

    slack_color = '#B22222';
    slack_message = "Failed: ${slack_message} Notifying @channel!\n\nError:\n```${err_str}```";
} finally {
    slackSend channel: "#e2e-tests", color: "${slack_color}", message: "${slack_message}";
}
