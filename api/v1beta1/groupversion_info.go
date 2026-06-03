/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

// Package v1beta1 contains API Schema definitions for the mc.k8s.lex.la v1beta1 API group.
// +kubebuilder:object:generate=true
// +groupName=mc.k8s.lex.la
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "mc.k8s.lex.la", Version: "v1beta1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// objectTypes collects the API types registered by each type file's init();
// addKnownTypes installs the whole set when AddToScheme runs.
//
//nolint:gochecknoglobals // mirrors the kubebuilder scheme-registration pattern
var objectTypes []runtime.Object

// addKnownTypes registers the collected types plus the GroupVersion metadata
// with the given scheme. This replaces controller-runtime's deprecated
// pkg/scheme.Builder so the api package depends only on apimachinery.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, objectTypes...)
	metav1.AddToGroupVersion(scheme, GroupVersion)

	return nil
}
