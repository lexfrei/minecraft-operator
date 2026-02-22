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

package controller

import (
	"bytes"
	"context"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// PodExecutor executes commands inside Kubernetes pods.
type PodExecutor interface {
	ExecInPod(ctx context.Context, namespace, podName, container string, command []string) ([]byte, error)
}

// K8sPodExecutor implements PodExecutor using client-go remotecommand (SPDY).
type K8sPodExecutor struct {
	config    *rest.Config
	clientset kubernetes.Interface
}

// NewK8sPodExecutor creates a new PodExecutor from a rest.Config.
func NewK8sPodExecutor(config *rest.Config) (*K8sPodExecutor, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create Kubernetes clientset")
	}

	return &K8sPodExecutor{
		config:    config,
		clientset: clientset,
	}, nil
}

// ExecInPod executes a command inside a pod and returns the combined stdout output.
func (e *K8sPodExecutor) ExecInPod(
	ctx context.Context,
	namespace, podName, container string,
	command []string,
) ([]byte, error) {
	req := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return nil, errors.Wrap(err, "failed to create SPDY executor")
	}

	var stdout, stderr bytes.Buffer

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "exec failed (stderr: %s)", stderr.String())
	}

	return stdout.Bytes(), nil
}
