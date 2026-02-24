/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
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
