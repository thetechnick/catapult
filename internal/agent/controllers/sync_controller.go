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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	catapultv1alpha1 "github.com/thetechnick/catapult/api/v1alpha1"
	"github.com/thetechnick/catapult/internal/owner"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const catapultControllerFinalizer string = "catapult.thetechnick.ninja/agent"

// SyncReconciler reconciles a Sync object
type SyncReconciler struct {
	Log logr.Logger
	client.Client
	Scheme *runtime.Scheme

	RemoteClusterName string
	RemoteClient      client.Client
	RemoteCache       cache.Cache
	ObjectGVK         schema.GroupVersionKind
}

// +kubebuilder:rbac:groups=catapult.thetechnick.ninja,resources=remotenamespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=catapult.thetechnick.ninja,resources=remotenamespaceclaims,verbs=get;list;watch

func (r *SyncReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	obj := r.newObject()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle Deletion
	if !obj.GetDeletionTimestamp().IsZero() {
		if err := r.handleDeletion(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("handling deletion: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Add Finalizer
	if AddFinalizer(obj, catapultControllerFinalizer) {
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating %s finalizers: %w", r.ObjectGVK.Kind, err)
		}
	}

	// There needs to be a Bound RemoteNamespace,
	// so we know where to put this object on the RemoteCluster
	remoteNamespaceName, err := r.getRemoteNamespaceForLocalNamespace(ctx, obj.GetNamespace())
	if err != nil {
		if errors.Is(err, &remoteNamespaceClaimNotBound{}) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		if errors.Is(err, &remoteNamespaceClaimNotFound{}) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	desiredObject := obj.DeepCopy()
	desiredObject.SetNamespace(remoteNamespaceName)

	currentObj := r.newObject()
	err = r.RemoteClient.Get(ctx, types.NamespacedName{
		Name:      desiredObject.GetName(),
		Namespace: desiredObject.GetNamespace(),
	}, currentObj)
	if err != nil && !k8serrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("getting %s: %w", r.ObjectGVK.Kind, err)
	}
	if k8serrors.IsNotFound(err) {
		if err := r.RemoteClient.Create(ctx, desiredObject); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating %s: %w", r.ObjectGVK.Kind, err)
		}
	}

	// Make sure we take ownership of the service cluster instance,
	// if the OwnerReference is not yet set.
	if _, err := owner.SetOwnerReference(
		obj, currentObj, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("setting owner reference: %w", err)
	}

	// Update existing service cluster instance
	// This is a bit complicated, because we want to support arbitrary fields and not only .spec.
	// Thats why we are updating everything, with the exception of .status and .metadata
	updatedObj := desiredObject.DeepCopy()
	if err := unstructured.SetNestedField(
		updatedObj.Object,
		currentObj.Object["metadata"], "metadata"); err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"updating %s .metadata: %w", r.ObjectGVK.Kind, err)
	}
	if err := unstructured.SetNestedField(
		updatedObj.Object,
		currentObj.Object["status"], "status"); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating %s .status: %w", r.ObjectGVK.Kind, err)
	}
	if err := r.RemoteClient.Update(ctx, updatedObj); err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"updating %s: %w", r.ObjectGVK.Kind, err)
	}

	if _, ok := obj.Object["status"]; !ok {
		return ctrl.Result{}, nil
	}

	// Sync Status from service cluster to management cluster
	if err := unstructured.SetNestedField(
		obj.Object,
		currentObj.Object["status"], "status"); err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"updating %s .status: %w", r.ObjectGVK.Kind, err)
	}
	if err = updateObservedGeneration(
		currentObj, obj); err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"update observedGeneration, by comparing %s to %s: %w",
			r.ObjectGVK.Kind, r.ObjectGVK.Kind, err)
	}
	if err = r.Status().Update(ctx, obj); err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"updating %s status: %w", r.ObjectGVK.Kind, err)
	}

	return ctrl.Result{}, nil
}

func (r *SyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(r.newObject()).
		Watches(
			source.NewKindWithCache(r.newObject(), r.RemoteCache),
			owner.EnqueueRequestForOwner(r.newObject(), mgr.GetScheme()),
		).
		Complete(r)
}

func (r *SyncReconciler) newObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.ObjectGVK)
	return obj
}

func (r *SyncReconciler) handleDeletion(
	ctx context.Context, obj *unstructured.Unstructured,
) error {
	remoteNamespace, err := r.getRemoteNamespaceForLocalNamespace(ctx, obj.GetNamespace())
	if err == nil {
		// Delete object on the RemoteCluster
		remoteObj := r.newObject()
		err := r.RemoteClient.Get(ctx, types.NamespacedName{
			Name:      remoteObj.GetName(),
			Namespace: remoteNamespace,
		}, remoteObj)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("getting %s: %w", r.ObjectGVK.Kind, err)
		}

		if err == nil && remoteObj.GetDeletionTimestamp().IsZero() {
			if err = r.RemoteClient.Delete(ctx, remoteObj); err != nil {
				return fmt.Errorf("deleting %s: %w", r.ObjectGVK.Kind, err)
			}
			return nil
		}

		if !k8serrors.IsNotFound(err) {
			// wait until object is really gone
			return nil
		}
	}
	if !errors.Is(err, &remoteNamespaceClaimNotFound{}) {
		return fmt.Errorf("getting RemoteNamespace for local Namespace: %w", err)
	}

	if RemoveFinalizer(obj, catapultControllerFinalizer) {
		if err := r.Update(ctx, obj); err != nil {
			return fmt.Errorf("updating %s finalizers: %w", r.ObjectGVK.Kind, err)
		}
	}
	return nil
}

func (r *SyncReconciler) getRemoteNamespaceForLocalNamespace(
	ctx context.Context, localNamespaceName string,
) (string, error) {
	rncList, err := catapultv1alpha1.ListRemoteNamespaceClaimsByLocalNamespaceName(
		ctx, r.Client, localNamespaceName)
	if err != nil {
		return "", err
	}

	var remoteNamespaceClaim *catapultv1alpha1.RemoteNamespaceClaim
	for _, claim := range rncList.Items {
		if claim.Spec.RemoteCluster.Name == r.RemoteClusterName {
			remoteNamespaceClaim = &claim
			break
		}
	}

	// Sanity check
	if remoteNamespaceClaim == nil {
		return "", &remoteNamespaceClaimNotFound{
			LocalNamespaceName: localNamespaceName,
		}
	}
	if remoteNamespaceClaim.Status.GetCondition(
		catapultv1alpha1.RemoteNamespaceClaimBound).Status != catapultv1alpha1.ConditionTrue {
		return "", &remoteNamespaceClaimNotBound{
			Name: remoteNamespaceClaim.Name,
		}
	}

	// Get RemoteNamespace object
	remoteNamespace := &catapultv1alpha1.RemoteNamespace{}
	if err := r.Get(ctx, types.NamespacedName{
		Name: remoteNamespaceClaim.Spec.RemoteNamespace.Name,
	}, remoteNamespace); err != nil {
		return "", fmt.Errorf("getting RemoteNamespace: %w", err)
	}
	if remoteNamespace.Spec.NamespaceInRemoteCluster == nil {
		return "", fmt.Errorf("RemoteNamespace .spec.namespaceInRemoteCluster is not set")
	}

	return remoteNamespace.Spec.NamespaceInRemoteCluster.Name, nil
}

type remoteNamespaceClaimNotFound struct {
	LocalNamespaceName string
}

func (e *remoteNamespaceClaimNotFound) Error() string {
	return fmt.Sprintf("RemoteNamespaceClaim for namespace %s not found", e.LocalNamespaceName)
}

func (e *remoteNamespaceClaimNotFound) Is(err error) bool {
	_, ok := err.(*remoteNamespaceClaimNotFound)
	return ok
}

type remoteNamespaceClaimNotBound struct {
	Name string
}

func (e *remoteNamespaceClaimNotBound) Is(err error) bool {
	_, ok := err.(*remoteNamespaceClaimNotBound)
	return ok
}

func (e *remoteNamespaceClaimNotBound) Error() string {
	return fmt.Sprintf("RemoteNamespaceClaim %s not Bound", e.Name)
}

// AddFinalizer adds the Finalizer to the metav1.Object if the Finalizer is not present.
func AddFinalizer(object metav1.Object, finalizer string) (changed bool) {
	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == finalizer {
			return false
		}
	}
	object.SetFinalizers(append(finalizers, finalizer))
	return true
}

// RemoveFinalizer removes the finalizer from the metav1.Object if the Finalizer is present.
func RemoveFinalizer(object metav1.Object, finalizer string) (changed bool) {
	finalizers := object.GetFinalizers()
	for i, f := range finalizers {
		if f == finalizer {
			finalizers = append(finalizers[:i], finalizers[i+1:]...)
			object.SetFinalizers(finalizers)
			return true
		}
	}
	return false
}

// updateObservedGeneration sets dest.Status.ObservedGeneration=dest.Generation,
// if src.Generation == src.Status.ObservedGeneration
func updateObservedGeneration(src, dest *unstructured.Unstructured) error {
	// check if source status is "up to date", by checking ObservedGeneration
	srcObservedGeneration, found, err := unstructured.NestedInt64(src.Object, "status", "observedGeneration")
	if err != nil {
		return fmt.Errorf("reading observedGeneration from %s: %w", src.GetKind(), err)
	}
	if !found {
		// observedGeneration field not present -> nothing to do
		return nil
	}

	if srcObservedGeneration != src.GetGeneration() {
		// observedGeneration is set, but it does not match
		// this means the status is not up to date
		// and we don't want to update observedGeneration on dest
		return nil
	}

	return unstructured.SetNestedField(
		dest.Object, dest.GetGeneration(), "status", "observedGeneration")
}
