/*
Copyright 2021.

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

// Package pki issues the certificates the operator needs from the ClusterIssuer
// it was given at install time.
//
// Each certificate is a cert-manager Certificate named after the secret it
// produces. A secret is the operator's when a Certificate the operator created
// issued it, which cert-manager records in the cert-manager.io/certificate-name
// annotation. Anything else under that name is the user's, and in issuer mode a
// user's secret is a conflict rather than something to adopt or overwrite.
package pki

import (
	"context"
	"fmt"
	"sort"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

// Finalizer is set on a CR whose certificates the operator issued. cert-manager
// leaves a secret behind when its Certificate is deleted, so the CR stays until
// its secrets are gone.
const Finalizer = "broker.arkmq.org/issued-certificates"

// Request describes a certificate to issue into the secret of the same name.
type Request struct {
	Name      string
	Namespace string
	// Owner of the Certificate, nil for one that outlives every CR, such as the
	// operator's own.
	Owner    client.Object
	DNSNames []string
}

func (r Request) key() types.NamespacedName {
	return types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
}

// ConflictError reports a resource under the name of a certificate to issue
// that the operator did not create.
type ConflictError struct {
	Kind string
	Key  types.NamespacedName
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %s was not issued by the operator: with %s set the operator issues every certificate, remove it or unset %s",
		e.Kind, e.Key, common.ClusterIssuerEnvVar, common.ClusterIssuerEnvVar)
}

// NotReadyError reports a certificate whose secret does not exist yet, with
// cert-manager's account of why.
type NotReadyError struct {
	Key    types.NamespacedName
	Reason string
}

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("certificate %s is not issued yet: %s", e.Key, e.Reason)
}

// Ensure issues the certificate from issuer unless it already is. It returns
// nil once the secret exists, a NotReadyError while it does not, and a
// ConflictError when the name is taken by something the operator did not issue.
//
// A secret that exists is good enough: renewals keep it in place, and a failing
// renewal is cert-manager's to report, not a reason to stop reconciling.
func Ensure(ctx context.Context, c client.Client, scheme *runtime.Scheme, issuer string, request Request) error {
	key := request.key()

	certificate := &cmv1.Certificate{}
	err := c.Get(ctx, key, certificate)
	if meta.IsNoMatchError(err) {
		return &NotReadyError{Key: key, Reason: "cert-manager is not installed"}
	}
	if errors.IsNotFound(err) {
		return create(ctx, c, scheme, issuer, request)
	}
	if err != nil {
		return err
	}

	if !issuedFor(certificate, request.Owner) {
		return &ConflictError{Kind: "Certificate", Key: key}
	}

	secret := &corev1.Secret{}
	err = c.Get(ctx, key, secret)
	if errors.IsNotFound(err) {
		return &NotReadyError{Key: key, Reason: notReadyReason(ctx, c, certificate)}
	}
	if err != nil {
		return err
	}
	if secret.Annotations[cmv1.CertificateNameKey] != key.Name {
		return &ConflictError{Kind: "Secret", Key: key}
	}
	return nil
}

func create(ctx context.Context, c client.Client, scheme *runtime.Scheme, issuer string, request Request) error {
	key := request.key()

	// checked before creating: cert-manager adopts and overwrites an existing
	// secret named in spec.secretName
	err := c.Get(ctx, key, &corev1.Secret{})
	if err == nil {
		return &ConflictError{Kind: "Secret", Key: key}
	}
	if !errors.IsNotFound(err) {
		return err
	}

	certificate := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      request.Name,
			Namespace: request.Namespace,
			Labels:    map[string]string{selectors.LabelAppKubernetesManagedBy: common.OperatorName},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: request.Name,
			// the common name is the identity a broker authorises, unguessable so
			// that access to the issuer alone does not let one mint it
			CommonName: uuid.NewString(),
			DNSNames:   request.DNSNames,
			IssuerRef: cmmetav1.ObjectReference{
				Name:  issuer,
				Kind:  cmv1.ClusterIssuerKind,
				Group: cmv1.SchemeGroupVersion.Group,
			},
		},
	}
	if request.Owner != nil {
		if err := controllerutil.SetControllerReference(request.Owner, certificate, scheme); err != nil {
			return err
		}
	}

	err = c.Create(ctx, certificate)
	if meta.IsNoMatchError(err) {
		return &NotReadyError{Key: key, Reason: "cert-manager is not installed"}
	}
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return &NotReadyError{Key: key, Reason: "certificate requested"}
}

// Release deletes a certificate the operator issued for request.Owner, then its
// secret. In that order, so that cert-manager does not issue the secret again.
// Anything the operator did not issue is left alone.
func Release(ctx context.Context, c client.Client, request Request) error {
	key := request.key()

	certificate := &cmv1.Certificate{}
	err := c.Get(ctx, key, certificate)
	if meta.IsNoMatchError(err) || errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !issuedFor(certificate, request.Owner) {
		return nil
	}

	if err := c.Delete(ctx, certificate); client.IgnoreNotFound(err) != nil {
		return err
	}

	secret := &corev1.Secret{}
	err = c.Get(ctx, key, secret)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if secret.Annotations[cmv1.CertificateNameKey] != key.Name {
		return nil
	}
	return client.IgnoreNotFound(c.Delete(ctx, secret))
}

// issuedFor tells whether the operator created certificate for owner. One with
// no owner is recognised by the operator's managed-by label.
func issuedFor(certificate *cmv1.Certificate, owner client.Object) bool {
	if owner == nil {
		return len(certificate.OwnerReferences) == 0 &&
			certificate.Labels[selectors.LabelAppKubernetesManagedBy] == common.OperatorName
	}
	return metav1.IsControlledBy(certificate, owner)
}

// notReadyReason is cert-manager's account of why certificate has no secret.
// The Certificate itself only says that it is issuing; the cause, such as a
// missing or unready issuer, is on its latest CertificateRequest.
func notReadyReason(ctx context.Context, c client.Client, certificate *cmv1.Certificate) string {
	requests := &cmv1.CertificateRequestList{}
	if err := c.List(ctx, requests, client.InNamespace(certificate.Namespace)); err == nil {
		owned := make([]cmv1.CertificateRequest, 0, len(requests.Items))
		for _, request := range requests.Items {
			if metav1.IsControlledBy(&request, certificate) {
				owned = append(owned, request)
			}
		}
		sort.Slice(owned, func(i, j int) bool {
			return owned[i].CreationTimestamp.After(owned[j].CreationTimestamp.Time)
		})
		for _, request := range owned {
			for _, condition := range request.Status.Conditions {
				if condition.Type == cmv1.CertificateRequestConditionReady && condition.Message != "" {
					return fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
				}
			}
		}
	}

	for _, condition := range certificate.Status.Conditions {
		if condition.Type == cmv1.CertificateConditionReady && condition.Message != "" {
			return fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
		}
	}
	return "certificate requested"
}
