/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	"github.com/onsi/gomega"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	consts "sigs.k8s.io/azuredisk-csi-driver/pkg/azureconstants"
	"sigs.k8s.io/azuredisk-csi-driver/pkg/azuredisk"
	"sigs.k8s.io/azuredisk-csi-driver/pkg/azureutils"
	"sigs.k8s.io/azuredisk-csi-driver/test/e2e/driver"
	"sigs.k8s.io/azuredisk-csi-driver/test/utils/azure"
	"sigs.k8s.io/azuredisk-csi-driver/test/utils/credentials"
	"sigs.k8s.io/cloud-provider-azure/pkg/provider"
)

const (
	kubeconfigEnvVar        = "KUBECONFIG"
	reportDirEnvVar         = "ARTIFACTS"
	testMigrationEnvVar     = "TEST_MIGRATION"
	testWindowsEnvVar       = "TEST_WINDOWS"
	cloudNameEnvVar         = "AZURE_CLOUD_NAME"
	defaultReportDir        = "/workspace/_artifacts"
	inTreeStorageClass      = "kubernetes.io/azure-disk"
	buildV2Driver           = "BUILD_V2"
	useOnlyDefaultScheduler = "USE_ONLY_DEFAULT_SCHEDULER"
	poll                    = time.Duration(2) * time.Second
	pollTimeout             = time.Duration(10) * time.Minute
)

var (
	azureCloud                  *provider.Cloud
	isUsingInTreeVolumePlugin   = os.Getenv(driver.AzureDriverNameVar) == inTreeStorageClass
	isTestingMigration          = os.Getenv(testMigrationEnvVar) != ""
	isWindowsCluster            = os.Getenv(testWindowsEnvVar) != ""
	isUsingCSIDriverV2          = strings.EqualFold(os.Getenv(buildV2Driver), "true")
	isUsingOnlyDefaultScheduler = os.Getenv(useOnlyDefaultScheduler) != ""
	isAzureStackCloud           = strings.EqualFold(os.Getenv(cloudNameEnvVar), "AZURESTACKCLOUD")
	skipClusterBootstrap        = flag.Bool("skip-cluster-bootstrap", false, "flag to indicate that we can skip cluster bootstrap.")
	location                    string
	supportsZRS                 bool
)

type testCmd struct {
	command  string
	args     []string
	startLog string
	endLog   string
}

var _ = ginkgo.BeforeSuite(func() {
	// k8s.io/kubernetes/test/e2e/framework requires env KUBECONFIG to be set
	// it does not fall back to defaults
	if os.Getenv(kubeconfigEnvVar) == "" {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		os.Setenv(kubeconfigEnvVar, kubeconfig)
	}
	handleFlags()
	framework.AfterReadingAllFlags(&framework.TestContext)

	// Default storage driver configuration is CSI. Freshly built
	// CSI driver is installed for that case.
	if isTestingMigration || !isUsingInTreeVolumePlugin {
		creds, err := credentials.CreateAzureCredentialFile()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		azureClient, err := azure.GetAzureClient(creds.Cloud, creds.SubscriptionID, creds.AADClientID, creds.TenantID, creds.AADClientSecret)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = azureClient.EnsureResourceGroup(context.Background(), creds.ResourceGroup, creds.Location, nil)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		location = creds.Location

		if location == "westus2" || location == "westeurope" {
			supportsZRS = true
		}

		// Install Azure Disk CSI Driver on cluster from project root
		e2eBootstrap := testCmd{
			command:  "make",
			args:     []string{"e2e-bootstrap"},
			startLog: "Installing Azure Disk CSI Driver...",
			endLog:   "Azure Disk CSI Driver installed",
		}

		createMetricsSVC := testCmd{
			command:  "make",
			args:     []string{"create-metrics-svc"},
			startLog: "create metrics service ...",
			endLog:   "metrics service created",
		}
		if *skipClusterBootstrap == false {
			execTestCmd([]testCmd{e2eBootstrap, createMetricsSVC})
		}

		driverOptions := azuredisk.DriverOptions{
			NodeID:                 os.Getenv("nodeid"),
			DriverName:             consts.DefaultDriverName,
			VolumeAttachLimit:      16,
			EnablePerfOptimization: false,
		}
		os.Setenv("AZURE_CREDENTIAL_FILE", credentials.TempAzureCredentialFilePath)
		kubeconfig := os.Getenv(kubeconfigEnvVar)
		kubeclient, err := azureutils.GetKubeClient(kubeconfig)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		azureCloud, err = azureutils.GetCloudProviderFromClient(kubeclient, driverOptions.CloudConfigSecretName, driverOptions.CloudConfigSecretNamespace, azuredisk.GetUserAgent(driverOptions.DriverName, driverOptions.CustomUserAgent, driverOptions.UserAgentSuffix), false)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
})

var _ = ginkgo.AfterSuite(func() {
	// Default storage driver configuration is CSI. Freshly built
	// CSI driver is installed for that case.
	if isTestingMigration || isUsingInTreeVolumePlugin {
		cmLog := testCmd{
			command:  "bash",
			args:     []string{"test/utils/controller-manager-log.sh"},
			startLog: "===================controller-manager log=======",
			endLog:   "===================================================",
		}
		execTestCmd([]testCmd{cmLog})
	}

	if isTestingMigration || !isUsingInTreeVolumePlugin {
		checkPodsRestart := testCmd{
			command:  "bash",
			args:     []string{"test/utils/check_driver_pods_restart.sh", "log"},
			startLog: "Check driver pods if restarts ...",
			endLog:   "Check successfully",
		}
		execTestCmd([]testCmd{checkPodsRestart})

		os := "linux"
		cloud := "azurepubliccloud"
		if isWindowsCluster {
			os = "windows"
		}
		if isAzureStackCloud {
			cloud = "azurestackcloud"
		}
		createExampleDeployment := testCmd{
			command:  "bash",
			args:     []string{"hack/verify-examples.sh", os, cloud},
			startLog: "create example deployments",
			endLog:   "example deployments created",
		}
		execTestCmd([]testCmd{createExampleDeployment})

		azurediskLogArgs := []string{"test/utils/azuredisk_log.sh", "azuredisk"}
		if isUsingCSIDriverV2 {
			azurediskLogArgs = append(azurediskLogArgs, "v2")
		}

		azurediskLog := testCmd{
			command:  "bash",
			args:     azurediskLogArgs,
			startLog: "===================azuredisk log===================",
			endLog:   "===================================================",
		}

		deleteMetricsSVC := testCmd{
			command:  "make",
			args:     []string{"delete-metrics-svc"},
			startLog: "delete metrics service...",
			endLog:   "metrics service deleted",
		}

		e2eTeardown := testCmd{
			command:  "make",
			args:     []string{"e2e-teardown"},
			startLog: "Uninstalling Azure Disk CSI Driver...",
			endLog:   "Azure Disk CSI Driver uninstalled",
		}

		if *skipClusterBootstrap {
			execTestCmd([]testCmd{azurediskLog})
		} else {
			execTestCmd([]testCmd{azurediskLog, deleteMetricsSVC, e2eTeardown})
		}

		if !isTestingMigration && !isUsingCSIDriverV2 {

			// install Azure Disk CSI Driver deployment scripts test
			installDriver := testCmd{
				command:  "bash",
				args:     []string{"deploy/install-driver.sh", "master", "windows,snapshot,local"},
				startLog: "===================install Azure Disk CSI Driver deployment scripts test===================",
				endLog:   "===================================================",
			}
			execTestCmd([]testCmd{installDriver})

			// run example deployment again
			createExampleDeployment := testCmd{
				command:  "bash",
				args:     []string{"hack/verify-examples.sh", os, cloud},
				startLog: "create example deployments#2",
				endLog:   "example deployments#2 created",
			}
			execTestCmd([]testCmd{createExampleDeployment})

			// uninstall Azure Disk CSI Driver deployment scripts test
			uninstallDriver := testCmd{
				command:  "bash",
				args:     []string{"deploy/uninstall-driver.sh", "master", "windows,snapshot,local"},
				startLog: "===================uninstall Azure Disk CSI Driver deployment scripts test===================",
				endLog:   "===================================================",
			}
			execTestCmd([]testCmd{uninstallDriver})
		}

		err := credentials.DeleteAzureCredentialFile()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
})

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	reportDir := os.Getenv(reportDirEnvVar)
	if reportDir == "" {
		reportDir = defaultReportDir
	}
	r := []ginkgo.Reporter{reporters.NewJUnitReporter(path.Join(reportDir, "junit_01.xml"))}
	ginkgo.RunSpecsWithDefaultAndCustomReporters(t, "AzureDisk CSI Driver End-to-End Tests", r)
}

func execTestCmd(cmds []testCmd) {
	err := os.Chdir("../..")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() {
		err := os.Chdir("test/e2e")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	projectRoot, err := os.Getwd()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(strings.HasSuffix(projectRoot, "azuredisk-csi-driver")).To(gomega.Equal(true))

	for _, cmd := range cmds {
		log.Println(cmd.startLog)
		cmdSh := exec.Command(cmd.command, cmd.args...)
		cmdSh.Dir = projectRoot
		cmdSh.Stdout = os.Stdout
		cmdSh.Stderr = os.Stderr
		err = cmdSh.Run()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		log.Println(cmd.endLog)
	}
}

func skipIfTestingInWindowsCluster() {
	if isWindowsCluster {
		ginkgo.Skip("test case not supported by Windows clusters")
	}
}

func skipIfUsingInTreeVolumePlugin() {
	if isUsingInTreeVolumePlugin {
		ginkgo.Skip("test case is only available for CSI drivers")
	}
}

func skipIfNotUsingCSIDriverV2() {
	if !isUsingCSIDriverV2 {
		ginkgo.Skip("test case is only available for CSI driver version v2")
	}
}

func skipIfOnAzureStackCloud() {
	if isAzureStackCloud {
		ginkgo.Skip("test case not supported on Azure Stack Cloud")
	}
}

func getSchedulerForE2E() string {
	if !isUsingCSIDriverV2 {
		return "default-scheduler"
	}
	if isUsingOnlyDefaultScheduler {
		return "default-scheduler"
	}
	return "csi-azuredisk-scheduler-extender"
}

func isZRSSupported() bool {
	return location == "westus2" || location == "westeurope"
}

func skipIfNotZRSSupported() {
	if !(isZRSSupported()) {
		ginkgo.Skip("test case not supported on regions without ZRS")
	}
}

func convertToPowershellorCmdCommandIfNecessary(command string) string {
	if !isWindowsCluster {
		return command
	}

	switch command {
	case "echo 'hello world' > /mnt/test-1/data && grep 'hello world' /mnt/test-1/data":
		return "echo 'hello world' | Out-File -FilePath C:\\mnt\\test-1\\data.txt; Get-Content C:\\mnt\\test-1\\data.txt | findstr 'hello world'"
	case "touch /mnt/test-1/data":
		return "echo $null >> C:\\mnt\\test-1\\data"
	case "while true; do echo $(date -u) >> /mnt/test-1/data; sleep 3600; done":
		return "while (1) { Add-Content -Encoding Ascii C:\\mnt\\test-1\\data.txt $(Get-Date -Format u); sleep 3600 }"
	case "echo 'hello world' >> /mnt/test-1/data && while true; do sleep 3600; done":
		return "echo 'hello world' | Out-File -Append -FilePath C:\\mnt\\test-1\\data.txt; Get-Content C:\\mnt\\test-1\\data.txt | findstr 'hello world'; Start-Sleep 3600"
	case "echo 'hello world' > /mnt/test-1/data && echo 'hello world' > /mnt/test-2/data && echo 'hello world' > /mnt/test-3/data && grep 'hello world' /mnt/test-1/data && grep 'hello world' /mnt/test-2/data && grep 'hello world' /mnt/test-3/data":
		return "echo 'hello world' | Out-File -FilePath C:\\mnt\\test-1\\data.txt; Get-Content C:\\mnt\\test-1\\data.txt | findstr 'hello world'; echo 'hello world' | Out-File -FilePath C:\\mnt\\test-2\\data.txt; Get-Content C:\\mnt\\test-2\\data.txt | findstr 'hello world'; echo 'hello world' | Out-File -FilePath C:\\mnt\\test-3\\data.txt; Get-Content C:\\mnt\\test-3\\data.txt | findstr 'hello world'"
	}

	return command
}

// handleFlags sets up all flags and parses the command line.
func handleFlags() {
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
	flag.Parse()
}
