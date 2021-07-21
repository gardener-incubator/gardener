/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

This file was copied and modified from the kubernetes/kubernetes project
https://github.com/kubernetes/kubernetes/blob/release-1.15/pkg/controller/certificates/approver/sarapprove.go

Modifications Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved.
*/

package certificatesigningrequest

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/pkg/utils"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	"github.com/go-logr/logr"
	authorizationv1beta1 "k8s.io/api/authorization/v1beta1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconciler struct {
	logger       logr.Logger
	gardenClient client.Client
}

func (r *reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := r.logger.WithValues("csr", request)

	csr := &certificatesv1beta1.CertificateSigningRequest{}
	if err := r.gardenClient.Get(ctx, request.NamespacedName, csr); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}

		logger.Error(err, "Unable to retrieve object from store")
		return reconcile.Result{}, err
	}

	if len(csr.Status.Certificate) != 0 {
		return reconcile.Result{}, nil
	}

	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1beta1.CertificateApproved {
			return reconcile.Result{}, nil
		}
		if c.Type == certificatesv1beta1.CertificateDenied {
			return reconcile.Result{}, nil
		}
	}

	x509cr, err := utils.DecodeCertificateRequest(csr.Spec.Request)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to parse csr %q: %w", csr.Name, err)
	}

	if !gutil.IsSeedClientCert(x509cr, csr.Spec.Usages) {
		return reconcile.Result{}, nil
	}

	logger.Info("Reconciling")

	attr := authorizationv1beta1.ResourceAttributes{
		Group:       "certificates.k8s.io",
		Resource:    "certificatesigningrequests",
		Verb:        "create",
		Subresource: "seedclient",
	}

	approved, err := authorize(ctx, r.gardenClient, csr, attr)
	if err != nil {
		return reconcile.Result{}, err
	}

	if approved {
		logger.Info("Auto-approving")

		csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1beta1.CertificateSigningRequestCondition{
			Type:    certificatesv1beta1.CertificateApproved,
			Reason:  "AutoApproved",
			Message: "Auto approving gardenlet client certificate after SubjectAccessReview.",
		})

		// TODO:
		// _, err := r.gardenClient.Kubernetes().CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(ctx, csr, kubernetes.DefaultUpdateOptions())
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, fmt.Errorf("recognized csr %q but subject access review was not approved", csr.Name)
}

func authorize(ctx context.Context, c client.Client, csr *certificatesv1beta1.CertificateSigningRequest, resourceAttributes authorizationv1beta1.ResourceAttributes) (bool, error) {
	extra := make(map[string]authorizationv1beta1.ExtraValue)
	for k, v := range csr.Spec.Extra {
		extra[k] = authorizationv1beta1.ExtraValue(v)
	}

	sar := &authorizationv1beta1.SubjectAccessReview{
		Spec: authorizationv1beta1.SubjectAccessReviewSpec{
			User:               csr.Spec.Username,
			UID:                csr.Spec.UID,
			Groups:             csr.Spec.Groups,
			Extra:              extra,
			ResourceAttributes: &resourceAttributes,
		},
	}

	if err := c.Create(ctx, sar); err != nil {
		return false, err
	}
	return sar.Status.Allowed, nil
}
