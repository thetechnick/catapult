/*
Copyright 2020 The Catapult authors.

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

package v1alpha1

import (
	"context"
	"fmt"

	runtime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RemoteNamespaceFieldIndex = "catapult.thetechnick.ninja/remote-namespace"
	LocalNamespaceFieldIndex  = "catapult.thetechnick.ninja/local-namespace"
)

func RegisterFieldIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	err := indexer.IndexField(ctx, &RemoteNamespace{}, RemoteNamespaceFieldIndex,
		func(obj runtime.Object) []string {
			remoteNamespace := obj.(*RemoteNamespace)
			if remoteNamespace.Spec.NamespaceInRemoteCluster != nil {
				return []string{remoteNamespace.Spec.NamespaceInRemoteCluster.Name}
			}
			return []string{}
		})
	if err != nil {
		return fmt.Errorf("index RemoteNamespace.spec.namespaceInRemoteCluster.Name: %w", err)
	}

	err = indexer.IndexField(ctx, &RemoteNamespaceClaim{}, LocalNamespaceFieldIndex,
		func(obj runtime.Object) []string {
			remoteNamespaceClaim := obj.(*RemoteNamespaceClaim)
			return []string{remoteNamespaceClaim.Spec.LocalNamespace.Name}
		})
	if err != nil {
		return fmt.Errorf("index RemoteNamespaceClaim.spec.localNamespace.Name: %w", err)
	}

	return nil
}

func ListRemoteNamespaceByRemoteNamespaceName(ctx context.Context, c client.Client, remoteNamespaceName string) (*RemoteNamespaceList, error) {
	remoteNamespaceList := &RemoteNamespaceList{}
	if err := c.List(ctx, remoteNamespaceList,
		client.MatchingFields{
			RemoteNamespaceFieldIndex: remoteNamespaceName,
		},
	); err != nil {
		return nil, err
	}
	return remoteNamespaceList, nil
}

func ListRemoteNamespaceClaimsByLocalNamespaceName(ctx context.Context, c client.Client, localNamespaceName string) (*RemoteNamespaceClaimList, error) {
	remoteNamespaceClaimList := &RemoteNamespaceClaimList{}
	if err := c.List(ctx, remoteNamespaceClaimList,
		client.MatchingFields{
			LocalNamespaceFieldIndex: localNamespaceName,
		},
	); err != nil {
		return nil, err
	}
	return remoteNamespaceClaimList, nil
}
