/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	umv1alpha1 "github.com/apeirora/showroom-simple-cluster/ubuntu-machines/api/v1alpha1"
)

// UbuntuMachineReconciler keeps a freshly-created UbuntuMachine's status
// in shape: empty phase + no boundClaimRef → phase=Available. The claim
// binder owns all subsequent transitions.
type UbuntuMachineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *UbuntuMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var m umv1alpha1.UbuntuMachine
	if err := r.Get(ctx, req.NamespacedName, &m); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !m.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	if m.Status.Phase != "" || m.Status.BoundClaimRef != nil {
		return ctrl.Result{}, nil
	}
	m.Status.Phase = umv1alpha1.MachineAvailable
	m.Status.ObservedGeneration = m.Generation
	if err := r.Status().Update(ctx, &m); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *UbuntuMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&umv1alpha1.UbuntuMachine{}).
		Named("ubuntumachine").
		Complete(r)
}
