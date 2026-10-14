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

package pki

import (
	"context"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	namespace = "ns"
	issuer    = "the-issuer"
)

var _ = Describe("certificates issued from a cluster issuer", func() {
	var (
		ctx     context.Context
		scheme  *runtime.Scheme
		c       client.Client
		owner   *corev1.ConfigMap
		request Request
		key     types.NamespacedName
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(cmv1.AddToScheme(scheme)).To(Succeed())
		c = fake.NewClientBuilder().WithScheme(scheme).Build()

		owner = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: namespace, UID: "owner-uid"}}
		request = Request{Name: "owner-broker-cert", Namespace: namespace, Owner: owner, DNSNames: []string{"owner.ns"}}
		key = types.NamespacedName{Name: request.Name, Namespace: namespace}
	})

	issuedSecret := func(certificateName string) *corev1.Secret {
		return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:        key.Name,
			Namespace:   namespace,
			Annotations: map[string]string{cmv1.CertificateNameKey: certificateName},
		}}
	}

	Context("Ensure", func() {
		It("requests a certificate the owner controls, with an unguessable common name", func() {
			err := Ensure(ctx, c, scheme, issuer, request)
			var notReady *NotReadyError
			Expect(err).To(BeAssignableToTypeOf(notReady))

			certificate := &cmv1.Certificate{}
			Expect(c.Get(ctx, key, certificate)).To(Succeed())
			Expect(certificate.Spec.SecretName).To(Equal(request.Name))
			Expect(certificate.Spec.DNSNames).To(Equal(request.DNSNames))
			Expect(certificate.Spec.IssuerRef).To(Equal(cmmetav1.ObjectReference{Name: issuer, Kind: "ClusterIssuer", Group: "cert-manager.io"}))
			Expect(metav1.IsControlledBy(certificate, owner)).To(BeTrue())
			_, err = uuid.Parse(certificate.Spec.CommonName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("gives each certificate its own common name", func() {
			other := Request{Name: "other-cert", Namespace: namespace, Owner: owner}
			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			Expect(Ensure(ctx, c, scheme, issuer, other)).NotTo(Succeed())

			first, second := &cmv1.Certificate{}, &cmv1.Certificate{}
			Expect(c.Get(ctx, key, first)).To(Succeed())
			Expect(c.Get(ctx, types.NamespacedName{Name: other.Name, Namespace: namespace}, second)).To(Succeed())
			Expect(first.Spec.CommonName).NotTo(Equal(second.Spec.CommonName))
		})

		It("keeps the common name it first requested", func() {
			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			before := &cmv1.Certificate{}
			Expect(c.Get(ctx, key, before)).To(Succeed())

			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			after := &cmv1.Certificate{}
			Expect(c.Get(ctx, key, after)).To(Succeed())
			Expect(after.Spec.CommonName).To(Equal(before.Spec.CommonName))
		})

		It("succeeds once the secret is issued", func() {
			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			Expect(c.Create(ctx, issuedSecret(key.Name))).To(Succeed())

			Expect(Ensure(ctx, c, scheme, issuer, request)).To(Succeed())
		})

		It("refuses a secret the user provided, without requesting anything", func() {
			user := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: namespace},
				Data:       map[string][]byte{"tls.crt": []byte("user")},
			}
			Expect(c.Create(ctx, user)).To(Succeed())

			err := Ensure(ctx, c, scheme, issuer, request)
			var conflict *ConflictError
			Expect(err).To(BeAssignableToTypeOf(conflict))
			Expect(err.Error()).To(ContainSubstring(key.String()))
			Expect(errors.IsNotFound(c.Get(ctx, key, &cmv1.Certificate{}))).To(BeTrue())
		})

		It("refuses a certificate the user created under that name", func() {
			user := &cmv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: namespace}}
			Expect(c.Create(ctx, user)).To(Succeed())

			var conflict *ConflictError
			Expect(Ensure(ctx, c, scheme, issuer, request)).To(BeAssignableToTypeOf(conflict))
		})

		It("refuses a secret the user put in place of an issued one", func() {
			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			Expect(c.Create(ctx, issuedSecret("someone-else"))).To(Succeed())

			var conflict *ConflictError
			Expect(Ensure(ctx, c, scheme, issuer, request)).To(BeAssignableToTypeOf(conflict))
		})

		It("recognises its own unowned certificate by label, and only that", func() {
			operator := Request{Name: "operator-cert", Namespace: namespace}
			Expect(Ensure(ctx, c, scheme, issuer, operator)).NotTo(Succeed())
			Expect(Ensure(ctx, c, scheme, issuer, operator)).To(BeAssignableToTypeOf(&NotReadyError{}))

			user := Request{Name: "user-operator-cert", Namespace: namespace}
			Expect(c.Create(ctx, &cmv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: user.Name, Namespace: namespace}})).To(Succeed())
			Expect(Ensure(ctx, c, scheme, issuer, user)).To(BeAssignableToTypeOf(&ConflictError{}))
		})

		It("reports why the latest request of the certificate is not issued", func() {
			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			certificate := &cmv1.Certificate{}
			Expect(c.Get(ctx, key, certificate)).To(Succeed())

			request := func(name string, age time.Duration, message string) *cmv1.CertificateRequest {
				cr := &cmv1.CertificateRequest{ObjectMeta: metav1.ObjectMeta{
					Name:              name,
					Namespace:         namespace,
					CreationTimestamp: metav1.NewTime(time.Now().Add(-age)),
				}}
				Expect(controllerutil.SetControllerReference(certificate, cr, scheme)).To(Succeed())
				cr.Status.Conditions = []cmv1.CertificateRequestCondition{{
					Type: cmv1.CertificateRequestConditionReady, Status: cmmetav1.ConditionFalse, Reason: "Pending", Message: message,
				}}
				return cr
			}
			Expect(c.Create(ctx, request("older", time.Hour, "stale"))).To(Succeed())
			Expect(c.Create(ctx, request("latest", time.Minute, `Referenced "ClusterIssuer" not found: clusterissuer.cert-manager.io "the-issuer" not found`))).To(Succeed())

			err := Ensure(ctx, c, scheme, issuer, Request{Name: key.Name, Namespace: namespace, Owner: owner})
			Expect(err).To(BeAssignableToTypeOf(&NotReadyError{}))
			Expect(err.Error()).To(ContainSubstring(`"the-issuer" not found`))
			Expect(err.Error()).NotTo(ContainSubstring("stale"))
		})
	})

	Context("Release", func() {
		It("deletes the certificate and the secret it issued", func() {
			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			Expect(c.Create(ctx, issuedSecret(key.Name))).To(Succeed())

			Expect(Release(ctx, c, request)).To(Succeed())
			Expect(errors.IsNotFound(c.Get(ctx, key, &cmv1.Certificate{}))).To(BeTrue())
			Expect(errors.IsNotFound(c.Get(ctx, key, &corev1.Secret{}))).To(BeTrue())
		})

		It("leaves alone what it did not issue", func() {
			Expect(c.Create(ctx, &cmv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: namespace}})).To(Succeed())
			Expect(c.Create(ctx, issuedSecret(key.Name))).To(Succeed())

			Expect(Release(ctx, c, request)).To(Succeed())
			Expect(c.Get(ctx, key, &cmv1.Certificate{})).To(Succeed())
			Expect(c.Get(ctx, key, &corev1.Secret{})).To(Succeed())
		})

		It("has nothing to do when nothing was issued", func() {
			Expect(Release(ctx, c, request)).To(Succeed())
		})
	})
})
