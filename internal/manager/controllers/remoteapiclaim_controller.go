/*
Copyright 2020 The Vedette authors.

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
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// RemoteNamespaceClaimReconciler reconciles a RemoteNamespaceClaim object
type RemoteNamespaceClaimReconciler struct {
	client.Client
	UncachedClient client.Client
	Log            logr.Logger
	Scheme         *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.vedette.io,resources=remotenamespaceclaim,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.vedette.io,resources=remotenamespaceclaim/status,verbs=get;update;patch

func (r *RemoteNamespaceClaimReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("interfaceclaim", req.NamespacedName)

	remoteNamespaceClient := &catapultv1alpha1.RemoteNamespaceClaim{}
	if err := r.Get(ctx, req.NamespacedName, remoteNamespaceClient); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Step 1:
	// Check if already bound
	if stop, err := r.checkAlreadyBound(ctx, log, remoteNamespaceClient); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	// Step 2:
	// Can we find an existing InterfaceInstance and bind to it?
	if stop, err := r.tryToBindInstance(ctx, log, remoteNamespaceClient); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	// Nothing matched :(
	// Claim remains unbound
	// retry later
	remoteNamespaceClient.Status.SetCondition(catapultv1alpha1.RemoteNamespaceClaimCondition{
		Type:    catapultv1alpha1.RemoteNamespaceClaimBound,
		Status:  catapultv1alpha1.ConditionFalse,
		Reason:  "NoMatchingInstance",
		Message: "No matching instance found for the given parameters.",
	})
	if err := r.Status().Update(ctx, remoteNamespaceClient); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating RemoteNamespaceClaim: %w", err)
	}
	return ctrl.Result{
		RequeueAfter: time.Second * 10,
	}, nil
}

func (r *RemoteNamespaceClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&catapultv1alpha1.RemoteNamespaceClaim{}).
		Watches(
			&source.Kind{Type: &catapultv1alpha1.RemoteNamespace{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
					remoteNamespace, ok := obj.Object.(*catapultv1alpha1.RemoteNamespace)
					if !ok || remoteNamespace.Spec.Claim == nil {
						return nil
					}

					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      remoteNamespace.Spec.Claim.Name,
								Namespace: remoteNamespace.Namespace,
							},
						},
					}
				}),
			},
		).
		Complete(r)
}

func (r *RemoteNamespaceClaimReconciler) checkAlreadyBound(
	ctx context.Context,
	log logr.Logger,
	claim *catapultv1alpha1.RemoteNamespaceClaim,
) (stop bool, err error) {
	// Check Bound Reference
	if claim.Spec.RemoteNamespace == nil {
		return false, nil
		// return false, fmt.Errorf("bound instance without Instance reference")
	}

	remoteNamespace := &catapultv1alpha1.RemoteNamespace{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      claim.Spec.RemoteNamespace.Name,
		Namespace: claim.Namespace,
	}, remoteNamespace)
	if k8serrors.IsNotFound(err) {
		// Reference lost
		claim.Status.SetCondition(catapultv1alpha1.RemoteNamespaceClaimCondition{
			Type:    catapultv1alpha1.RemoteNamespaceClaimLost,
			Status:  catapultv1alpha1.ConditionTrue,
			Reason:  "InstanceLost",
			Message: "Bound RemoteNamespace can no longer be found.",
		})
		if err = r.Status().Update(ctx, claim); err != nil {
			return false, fmt.Errorf("updating claim status: %w", err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting Instance: %w", err)
	}

	// Everything is alright!
	claim.Status.SetCondition(catapultv1alpha1.RemoteNamespaceClaimCondition{
		Type:    catapultv1alpha1.RemoteNamespaceClaimLost,
		Status:  catapultv1alpha1.ConditionFalse,
		Reason:  "InstanceFound",
		Message: "Bound RemoteNamespace can be found.",
	})
	return true, nil
}

// Try to find a matching InterfaceInstance and bind to it.
func (r *RemoteNamespaceClaimReconciler) tryToBindInstance(
	ctx context.Context,
	log logr.Logger,
	claim *catapultv1alpha1.RemoteNamespaceClaim,
) (stop bool, err error) {
	instanceSelector, err := metav1.LabelSelectorAsSelector(&claim.Spec.Selector)
	if err != nil {
		// should have been covered by validation
		return false, fmt.Errorf("parsing LabelSelector as Selector: %w", err)
	}

	if claim.Spec.RemoteNamespace == nil {
		// Find a matching InterfaceInstance to bind to
		remoteNamespaceList := &catapultv1alpha1.RemoteNamespaceList{}
		if err := r.List(
			ctx,
			remoteNamespaceList,
			client.InNamespace(claim.Namespace),
			client.MatchingLabelsSelector{Selector: instanceSelector},
		); err != nil {
			return false, fmt.Errorf("listing InterfaceInstances: %w", err)
		}
		for _, remoteNamespace := range remoteNamespaceList.Items {
			if claimMatchesInstance(log, claim, &remoteNamespace) {
				claim.Spec.RemoteNamespace = &catapultv1alpha1.ObjectReference{
					Name: remoteNamespace.Name,
				}
				break
			}
		}
	}

	if claim.Spec.RemoteNamespace == nil {
		// no matching RemoteNamespace found
		// claim remains unbound
		return false, nil
	}

	// Bind to an RemoteNamespace
	if err = r.Update(ctx, claim); err != nil {
		return false, fmt.Errorf("updating Claim: %w", err)
	}

	instance := &catapultv1alpha1.RemoteNamespace{}
	if err = r.Get(ctx, types.NamespacedName{
		Name:      claim.Spec.RemoteNamespace.Name,
		Namespace: claim.Namespace,
	}, instance); err != nil {
		return false, fmt.Errorf("getting supposed-to-be bound Instance: %w", err)
	}
	if instance.Spec.Claim != nil &&
		instance.Spec.Claim.Name != claim.Name {
		// oh-no! This is not supposed to happen.
		return true, fmt.Errorf(
			"tried to bind to instance %s already bound to claim %s: %w", instance.Name, instance.Spec.Claim.Name, err)
	}
	instance.Spec.Claim = &catapultv1alpha1.ObjectReference{
		Name: claim.Name,
	}
	if err = r.Update(ctx, instance); err != nil {
		return false, fmt.Errorf("updating Instance: %w", err)
	}

	// update status
	claim.Status.SetCondition(catapultv1alpha1.RemoteNamespaceClaimCondition{
		Type:    catapultv1alpha1.RemoteNamespaceClaimBound,
		Status:  catapultv1alpha1.ConditionTrue,
		Reason:  "Bound",
		Message: "Bound to instance.",
	})
	if err = r.Status().Update(ctx, claim); err != nil {
		return false, fmt.Errorf("update claim status: %w", err)
	}
	return
}

func claimMatchesInstance(
	log logr.Logger,
	claim *catapultv1alpha1.RemoteNamespaceClaim,
	remoteNamespace *catapultv1alpha1.RemoteNamespace,
) bool {
	// Check instance status
	bound := remoteNamespace.Status.GetCondition(catapultv1alpha1.RemoteNamespaceBound)
	if bound.Status == catapultv1alpha1.ConditionTrue ||
		remoteNamespace.Spec.Claim == nil ||
		remoteNamespace.Spec.Claim.Name != "" {
		// already bound
		return false
	}
	if bound.Status == catapultv1alpha1.ConditionFalse &&
		bound.Reason == "Released" {
		// has been released from a previous claim
		// Released instances need to be bound to a new claim explicitly
		return false
	}

	if remoteNamespace.Status.GetCondition(catapultv1alpha1.RemoteNamespaceAvailable).Status != catapultv1alpha1.ConditionTrue {
		// unavailable
		return false
	}

	if remoteNamespace.Spec.RemoteCluster.Name != claim.Spec.RemoteCluster.Name {
		return false
	}
	return true
}
