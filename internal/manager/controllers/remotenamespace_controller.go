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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	catapultv1alpha1 "github.com/thetechnick/catapult/api/v1alpha1"
)

const claimLabel = "catapult.thetechnick.ninja/claim"

// RemoteNamespaceReconciler reconciles a RemoteNamespace object.
type RemoteNamespaceReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=catapult.thetechnick.ninja,resources=remotenamespaces,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=catapult.thetechnick.ninja,resources=remotenamespaces/status,verbs=get;update

func (r *RemoteNamespaceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	_ = r.Log.WithValues("remoteapi", req.NamespacedName)

	remoteNamespace := &catapultv1alpha1.RemoteNamespace{}
	if errors.IsNotFound(r.Get(ctx, req.NamespacedName, remoteNamespace)) {
		return ctrl.Result{}, nil
	}

	// Check for deletion
	if stop, err := r.checkForDeletion(ctx, remoteNamespace); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	// Provisioner Handling
	if remoteNamespace.Spec.RemoteNamespaceClass != "" &&
		remoteNamespace.Status.GetCondition(
			catapultv1alpha1.RemoteNamespaceAvailable,
		).LastTransitionTime.Time.IsZero() {
		// No class -> nothing to do
		return ctrl.Result{}, nil
	}

	remoteNamespace.Status.SetCondition(catapultv1alpha1.RemoteNamespaceCondition{
		Type:    catapultv1alpha1.RemoteNamespaceAvailable,
		Status:  catapultv1alpha1.ConditionTrue,
		Reason:  "AllComponentsReady",
		Message: "RemoteNamespaces with no Class are always considered Ready.",
	})
	remoteNamespace.Status.SetCondition(catapultv1alpha1.RemoteNamespaceCondition{
		Type:    catapultv1alpha1.RemoteNamespaceReady,
		Status:  catapultv1alpha1.ConditionTrue,
		Reason:  "AllComponentsReady",
		Message: "RemoteNamespaces with no Class are always considered Ready.",
	})
	remoteNamespace.Status.ObservedGeneration = remoteNamespace.Generation
	if err := r.Status().Update(ctx, remoteNamespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating RemoteNamespace status: %w", err)
	}

	return ctrl.Result{}, nil
}

// Check if the Claim belonging to this RemoteNamespace still exists or if it was deleted.
func (r *RemoteNamespaceReconciler) checkForDeletion(
	ctx context.Context,
	remoteNamespace *catapultv1alpha1.RemoteNamespace,
) (stop bool, err error) {
	if remoteNamespace.Spec.Claim == nil &&
		remoteNamespace.Status.GetCondition(catapultv1alpha1.RemoteNamespaceBound).Status != catapultv1alpha1.ConditionTrue {
		// Instance is not yet bound -> nothing to check
		return false, nil
	}

	claim := &catapultv1alpha1.RemoteNamespaceClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      remoteNamespace.Spec.Claim.Name,
		Namespace: remoteNamespace.Namespace,
	}, claim); err == nil {
		// claim is present
		remoteNamespace.Status.SetCondition(catapultv1alpha1.RemoteNamespaceCondition{
			Type:    catapultv1alpha1.RemoteNamespaceBound,
			Status:  catapultv1alpha1.ConditionTrue,
			Reason:  "Bound",
			Message: fmt.Sprintf("Bound by RemoteNamespaceClaim %s.", claim.Name),
		})
		if err = r.Status().Update(ctx, remoteNamespace); err != nil {
			return false, fmt.Errorf("update claim status: %w", err)
		}

		return true, nil
	} else if !errors.IsNotFound(err) {
		// some other error
		return false, fmt.Errorf("getting claim: %w", err)
	}

	// Claim was not found
	switch remoteNamespace.Spec.ReclaimPolicy {
	case catapultv1alpha1.RemoteNamespaceReclaimPolicyDelete:
		if err := r.Delete(ctx, remoteNamespace); err != nil {
			return false, fmt.Errorf("enforcing reclaim policy, deleting remoteNamespace: %w", err)
		}
		return true, nil

	case catapultv1alpha1.RemoteNamespaceReclaimPolicyRetain:
		remoteNamespace.Status.SetCondition(catapultv1alpha1.RemoteNamespaceCondition{
			Type:    catapultv1alpha1.RemoteNamespaceBound,
			Status:  catapultv1alpha1.ConditionFalse,
			Reason:  "Released",
			Message: fmt.Sprintf("The previously bound claim %s has been deleted.", remoteNamespace.Spec.Claim.Name),
		})
		if err := r.Status().Update(ctx, remoteNamespace); err != nil {
			return false, fmt.Errorf("updating remoteNamespace status: %w", err)
		}
		delete(remoteNamespace.Labels, claimLabel)
		remoteNamespace.Spec.Claim.Name = ""
		if err := r.Update(ctx, remoteNamespace); err != nil {
			return false, fmt.Errorf("updating remoteNamespace: %w", err)
		}

		return true, nil
	default:
		// unknown ReclaimPolicy -> do nothing
		return false, nil
	}
}

func (r *RemoteNamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&catapultv1alpha1.RemoteNamespace{}).
		Watches(
			&source.Kind{Type: &catapultv1alpha1.RemoteNamespaceClaim{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
					claim, ok := obj.Object.(*catapultv1alpha1.RemoteNamespaceClaim)
					if !ok || claim.Spec.RemoteNamespace == nil {
						return nil
					}

					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      claim.Spec.RemoteNamespace.Name,
								Namespace: claim.Namespace,
							},
						},
					}
				}),
			},
		).
		Complete(r)
}
