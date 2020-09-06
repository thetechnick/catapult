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

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	catapultv1alpha1 "github.com/thetechnick/catapult/api/v1alpha1"
	"github.com/thetechnick/catapult/internal/owner"
)

// AdoptionReconciler reconciles a Sync object
type AdoptionReconciler struct {
	Log logr.Logger
	client.Client
	Scheme *runtime.Scheme

	RemoteClusterName string
	RemoteClient      client.Client
	RemoteCache       cache.Cache
	ObjectGVK         schema.GroupVersionKind
}

// +kubebuilder:rbac:groups=catapult.thetechnick.ninja,resources=remotenamespaces,verbs=get;list;watch

func (r *AdoptionReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	_ = r.Log.WithValues("remoteapi", req.NamespacedName)

	obj := r.newObject()
	if err := r.RemoteClient.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if owner.IsOwned(obj) ||
		!obj.GetDeletionTimestamp().IsZero() {
		// the object is already owned or was deleted, so we don't want to do anything.
		// otherwise we might recreate the owning object preventing the deletion of this instance.
		return ctrl.Result{}, nil
	}

	// Check RemoteNamespace to find the local namespace
	remoteNamespaceList, err := catapultv1alpha1.ListRemoteNamespaceByRemoteNamespaceName(ctx, r.Client, obj.GetNamespace())
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting RemoteNamespace for namespace %q in remote cluster: %w", obj.GetNamespace(), err)
	}
	var remoteNamespace *catapultv1alpha1.RemoteNamespace
	for _, rns := range remoteNamespaceList.Items {
		if rns.Spec.RemoteCluster.Name == r.RemoteClusterName {
			remoteNamespace = &rns
			break
		}
	}
	if remoteNamespace == nil {
		return ctrl.Result{}, fmt.Errorf("RemoteNamespace not found for namespace %q in remote cluster", obj.GetNamespace())
	}
	if remoteNamespace.Status.GetCondition(
		catapultv1alpha1.RemoteNamespaceBound).Status != catapultv1alpha1.ConditionTrue || remoteNamespace.Spec.Claim == nil {
		return ctrl.Result{}, fmt.Errorf("RemoteNamespace %s not bound", remoteNamespace.Name)
	}
	remoteNamespaceClaim := &catapultv1alpha1.RemoteNamespaceClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Name: remoteNamespace.Spec.Claim.Name,
	}, remoteNamespaceClaim); err != nil {
		return ctrl.Result{}, fmt.Errorf("getting RemoteNamespaceClaim: %w", err)
	}

	// Reconcile new object owner
	desiredObject := obj.DeepCopy()
	desiredObject.SetNamespace(remoteNamespaceClaim.Spec.LocalNamespace.Name)
	desiredObject.SetOwnerReferences(nil)
	desiredObject.SetResourceVersion("")

	currentObj := r.newObject()
	err = r.Get(ctx, types.NamespacedName{
		Name:      desiredObject.GetName(),
		Namespace: desiredObject.GetNamespace(),
	}, currentObj)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("getting %s: %w", r.ObjectGVK.Kind, err)
	}
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, desiredObject); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating %s: %w", r.ObjectGVK.Kind, err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *AdoptionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New(
		strings.ToLower(r.ObjectGVK.Kind+" adoption"),
		mgr, controller.Options{
			Reconciler: r,
		})
	if err != nil {
		return fmt.Errorf("creating controller: %w", err)
	}

	return c.Watch(
		source.NewKindWithCache(r.newObject(), r.RemoteCache),
		&handler.EnqueueRequestForObject{},
		PredicateFn(func(obj runtime.Object) bool {
			// we are only interested in unowned objects
			meta, ok := obj.(metav1.Object)
			if !ok {
				return false
			}
			return !owner.IsOwned(meta)
		}))
}

func (r *AdoptionReconciler) newObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.ObjectGVK)
	return obj
}

type PredicateFn func(obj runtime.Object) bool

func (p PredicateFn) Create(ev event.CreateEvent) bool {
	return p(ev.Object)
}

func (p PredicateFn) Delete(ev event.DeleteEvent) bool {
	return p(ev.Object)
}

func (p PredicateFn) Update(ev event.UpdateEvent) bool {
	return p(ev.ObjectNew)
}

func (p PredicateFn) Generic(ev event.GenericEvent) bool {
	return p(ev.Object)
}

var _ predicate.Predicate = (PredicateFn)(nil)
