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

package owner

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// OwnerNameLabel references the name of the owner of this object.
	OwnerNameLabel = "catapult.thetechnick.ninja/owner-name"
	// OwnerNamespaceLabel references the namespace of the owner of this object.
	OwnerNamespaceLabel = "catapult.thetechnick.ninja/owner-namespace"
	// OwnerTypeLabel references the type of the owner of this object.
	OwnerTypeLabel = "catapult.thetechnick.ninja/owner-type"
)

// SetOwnerReference sets a the owner as owner of object.
func SetOwnerReference(owner, object runtime.Object, scheme *runtime.Scheme) (changed bool, err error) {
	objectAccessor, err := meta.Accessor(object)
	if err != nil {
		panic(fmt.Errorf("cannot get accessor for %T :%w", object, err))
	}

	labels := objectAccessor.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	ownerLabels := labelsForOwner(owner, scheme)
	for k, v := range ownerLabels {
		if labels[k] == "" {
			// label was not set before.
			changed = true
		} else if labels[k] != v {
			// label is overridden.
			return changed, fmt.Errorf("tried to override owner reference: %s=%s to =%s", k, labels[k], v)
		}
		labels[k] = v
	}
	objectAccessor.SetLabels(labels)
	return
}

// IsOwned checks if any owners claim ownership of this object.
func IsOwned(object metav1.Object) (owned bool) {
	l := object.GetLabels()
	if l == nil {
		return false
	}

	return l[OwnerNameLabel] != "" && l[OwnerNamespaceLabel] != "" && l[OwnerTypeLabel] != ""
}

// EnqueueRequestForOwner queues a request for the owner of an object
func EnqueueRequestForOwner(ownerType runtime.Object, scheme *runtime.Scheme) handler.EventHandler {
	return &handler.EnqueueRequestsFromMapFunc{
		ToRequests: requestHandlerForOwner(ownerType, scheme),
	}
}

type generalizedListOption interface {
	client.ListOption
	client.DeleteAllOfOption
}

// OwnedBy returns a list filter to fetch owned objects.
func OwnedBy(owner runtime.Object, scheme *runtime.Scheme) generalizedListOption {
	return client.MatchingLabels(labelsForOwner(owner, scheme))
}

func requestHandlerForOwner(ownerType runtime.Object, scheme *runtime.Scheme) handler.ToRequestsFunc {
	gvk, err := apiutil.GVKForObject(ownerType, scheme)
	if err != nil {
		panic(fmt.Sprintf("cannot get GVK for owner (type %T)", ownerType))
	}

	gk := gvk.GroupKind().String()

	return func(obj handler.MapObject) (requests []reconcile.Request) {
		labels := obj.Meta.GetLabels()
		if labels == nil {
			return
		}

		ownerName, ok := labels[OwnerNameLabel]
		if !ok {
			return
		}
		ownerNamespace, ok := labels[OwnerNamespaceLabel]
		if !ok {
			return
		}
		ownerType, ok := labels[OwnerTypeLabel]
		if !ok {
			return
		}

		if ownerType != gk {
			return
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ownerName,
				Namespace: ownerNamespace,
			},
		})
		return
	}
}

func labelsForOwner(obj runtime.Object, scheme *runtime.Scheme) map[string]string {
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		panic(fmt.Sprintf("cannot deduce GVK for owner (type %T)", obj))
	}

	metaAccessor, err := meta.Accessor(obj)
	if err != nil {
		panic(err)
	}

	return map[string]string{
		OwnerNameLabel:      metaAccessor.GetName(),
		OwnerNamespaceLabel: metaAccessor.GetNamespace(),
		OwnerTypeLabel:      gvk.GroupKind().String(),
	}
}
