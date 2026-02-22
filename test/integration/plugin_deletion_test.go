//go:build integration

/*
Copyright 2025.

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

package integration

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// kindContext is the kubectl context for the Kind cluster.
const kindContext = "kind-mc-test"

// testPodNamespace is the namespace for plugin deletion integration tests.
const testPodNamespace = "plugin-deletion-test"

// testPodName is the name of the busybox pod used for filesystem testing.
const testPodName = "test-server-0"

// testContainerName matches the container name used by the operator.
const testContainerName = "papermc"

// run executes a command and returns output.
func run(cmd *exec.Cmd) (string, error) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed: %s: %w", cmd.Args, string(output), err)
	}

	return string(output), nil
}

// execInPod runs a command inside the test pod.
func execInPod(command string) (string, error) {
	cmd := exec.Command("kubectl", "--context", kindContext,
		"--namespace", testPodNamespace,
		"exec", testPodName,
		"--container", testContainerName,
		"--", "sh", "-c", command)

	return run(cmd)
}

// fileExists checks if a file exists in the test pod.
func fileExists(path string) bool {
	cmd := exec.Command("kubectl", "--context", kindContext,
		"--namespace", testPodNamespace,
		"exec", testPodName,
		"--container", testContainerName,
		"--", "test", "-f", path)
	_, err := run(cmd)

	return err == nil
}

var _ = Describe("Plugin deletion edge cases", Ordered, func() {
	BeforeAll(func() {
		By("creating test namespace")
		cmd := exec.Command("kubectl", "--context", kindContext,
			"create", "namespace", testPodNamespace)
		_, _ = run(cmd)

		By("creating a busybox pod that mimics PaperMC directory structure")
		cmd = exec.Command("kubectl", "--context", kindContext,
			"--namespace", testPodNamespace,
			"run", testPodName,
			"--image=busybox:1.37",
			"--labels=app=papermc",
			"--overrides", fmt.Sprintf(`{
				"spec": {
					"containers": [{
						"name": "%s",
						"image": "busybox:1.37",
						"command": ["sh", "-c", "mkdir -p /data/plugins/update && sleep 3600"]
					}]
				}
			}`, testContainerName))
		_, err := run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test pod")

		By("waiting for the test pod to be ready")
		waitForPod := func(g Gomega) {
			cmd := exec.Command("kubectl", "--context", kindContext,
				"--namespace", testPodNamespace,
				"get", "pod", testPodName,
				"--output", "jsonpath={.status.phase}")
			output, err := run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("Running"))
		}
		Eventually(waitForPod, 60*time.Second, time.Second).Should(Succeed())
	})

	AfterAll(func() {
		By("cleaning up test namespace")
		cmd := exec.Command("kubectl", "--context", kindContext,
			"delete", "namespace", testPodNamespace,
			"--ignore-not-found", "--timeout=60s")
		_, _ = run(cmd)
	})

	Context("JAR in update/ only (server not restarted)", func() {
		It("should delete JAR from update/ directory", func() {
			By("creating a JAR file only in update/")
			_, err := execInPod("echo 'fake-jar' > /data/plugins/update/TestPlugin.jar")
			Expect(err).NotTo(HaveOccurred())

			By("verifying the file exists only in update/")
			Expect(fileExists("/data/plugins/update/TestPlugin.jar")).To(BeTrue())
			Expect(fileExists("/data/plugins/TestPlugin.jar")).To(BeFalse())

			By("running the same rm command the operator uses")
			_, err = execInPod(
				"rm -f /data/plugins/TestPlugin.jar /data/plugins/update/TestPlugin.jar")
			Expect(err).NotTo(HaveOccurred(),
				"rm -f should succeed even when plugins/ file doesn't exist")

			By("verifying the JAR is deleted from update/")
			Expect(fileExists("/data/plugins/update/TestPlugin.jar")).To(BeFalse(),
				"JAR should be deleted from update/ directory")
		})
	})

	Context("JAR in both plugins/ and update/ (two versions coexist)", func() {
		It("should delete JARs from both directories", func() {
			By("creating JAR files in both directories")
			_, err := execInPod("echo 'old-version' > /data/plugins/BlueMap.jar")
			Expect(err).NotTo(HaveOccurred())
			_, err = execInPod("echo 'new-version' > /data/plugins/update/BlueMap.jar")
			Expect(err).NotTo(HaveOccurred())

			By("verifying both files exist")
			Expect(fileExists("/data/plugins/BlueMap.jar")).To(BeTrue())
			Expect(fileExists("/data/plugins/update/BlueMap.jar")).To(BeTrue())

			By("running the same rm command the operator uses")
			_, err = execInPod(
				"rm -f /data/plugins/BlueMap.jar /data/plugins/update/BlueMap.jar")
			Expect(err).NotTo(HaveOccurred())

			By("verifying both JARs are deleted")
			Expect(fileExists("/data/plugins/BlueMap.jar")).To(BeFalse(),
				"JAR should be deleted from plugins/ directory")
			Expect(fileExists("/data/plugins/update/BlueMap.jar")).To(BeFalse(),
				"JAR should be deleted from update/ directory")
		})
	})

	Context("JAR in plugins/ only (normal case, post-restart)", func() {
		It("should delete JAR from plugins/ directory", func() {
			By("creating a JAR file only in plugins/")
			_, err := execInPod("echo 'installed' > /data/plugins/EssentialsX.jar")
			Expect(err).NotTo(HaveOccurred())

			By("verifying the file exists only in plugins/")
			Expect(fileExists("/data/plugins/EssentialsX.jar")).To(BeTrue())
			Expect(fileExists("/data/plugins/update/EssentialsX.jar")).To(BeFalse())

			By("running the same rm command the operator uses")
			_, err = execInPod(
				"rm -f /data/plugins/EssentialsX.jar /data/plugins/update/EssentialsX.jar")
			Expect(err).NotTo(HaveOccurred(),
				"rm -f should succeed even when update/ file doesn't exist")

			By("verifying the JAR is deleted from plugins/")
			Expect(fileExists("/data/plugins/EssentialsX.jar")).To(BeFalse(),
				"JAR should be deleted from plugins/ directory")
		})
	})

	Context("No JAR anywhere (never installed plugin)", func() {
		It("should succeed when no files exist", func() {
			By("ensuring no files exist for this plugin")
			Expect(fileExists("/data/plugins/NeverInstalled.jar")).To(BeFalse())
			Expect(fileExists("/data/plugins/update/NeverInstalled.jar")).To(BeFalse())

			By("running rm -f on non-existent files")
			_, err := execInPod(
				"rm -f /data/plugins/NeverInstalled.jar /data/plugins/update/NeverInstalled.jar")
			Expect(err).NotTo(HaveOccurred(),
				"rm -f should succeed when no files exist (idempotent)")
		})
	})

	Context("Multiple plugins deleted simultaneously", func() {
		It("should handle batch deletion of mixed scenarios", func() {
			By("setting up multiple plugins in different states")
			_, err := execInPod("echo 'a' > /data/plugins/PluginA.jar")
			Expect(err).NotTo(HaveOccurred())
			_, err = execInPod("echo 'b' > /data/plugins/update/PluginB.jar")
			Expect(err).NotTo(HaveOccurred())
			_, err = execInPod("echo 'c-old' > /data/plugins/PluginC.jar")
			Expect(err).NotTo(HaveOccurred())
			_, err = execInPod("echo 'c-new' > /data/plugins/update/PluginC.jar")
			Expect(err).NotTo(HaveOccurred())

			By("deleting all plugins using the operator's rm pattern")
			for _, name := range []string{"PluginA.jar", "PluginB.jar", "PluginC.jar"} {
				rmCmd := fmt.Sprintf(
					"rm -f /data/plugins/%s /data/plugins/update/%s", name, name)
				_, err = execInPod(rmCmd)
				Expect(err).NotTo(HaveOccurred(), "Failed to delete "+name)
			}

			By("verifying all plugins are cleaned up")
			for _, path := range []string{
				"/data/plugins/PluginA.jar",
				"/data/plugins/update/PluginB.jar",
				"/data/plugins/PluginC.jar",
				"/data/plugins/update/PluginC.jar",
			} {
				Expect(fileExists(path)).To(BeFalse(), "File should be deleted: "+path)
			}

			By("verifying update/ directory itself still exists")
			_, err = execInPod("test -d /data/plugins/update")
			Expect(err).NotTo(HaveOccurred(),
				"update/ directory should not be removed, only files inside it")
		})
	})
})
