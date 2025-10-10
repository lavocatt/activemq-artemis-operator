/*
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
// +kubebuilder:docs-gen:collapse=Apache License
package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	brokerv1beta1 "github.com/arkmq-org/activemq-artemis-operator/api/v1beta1"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	artemis_client "github.com/arkmq-org/activemq-artemis-operator/pkg/utils/artemis"
	"github.com/arkmq-org/activemq-artemis-operator/pkg/utils/jolokia"
	"github.com/arkmq-org/activemq-artemis-operator/pkg/utils/jolokia_client"
	"github.com/arkmq-org/activemq-artemis-operator/pkg/utils/selectors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestValidate(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			ResourceTemplates: []brokerv1beta1.ResourceTemplate{
				{
					// reserved key
					Labels: map[string]string{selectors.LabelAppKey: "myAppKey"},
				},
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedReservedLabelReason)
	assert.True(t, strings.Contains(condition.Message, "Templates[0]"))
}

func TestValidateBrokerPropsDuplicate(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			BrokerProperties: []string{
				"min=X",
				"min=y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "min"))
}

func TestValidateBrokerPropsDuplicateOnFirstEquals(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			BrokerProperties: []string{
				"nameWith\\=equals_not_matched=X",
				"nameWith\\=equals_not_matched=Y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "nameWith"))
}

func TestValidateBrokerPropsDuplicateOnFirstEqualsIncorrectButUnrealisticForOurBrokerConfigUsecase(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			BrokerProperties: []string{
				"nameWith\\=equals_A_not_matched=X",
				"nameWith\\=equals_B_not_matched=Y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "nameWith"))
}

func TestStatusPodsCheckCached(t *testing.T) {

	replicas := int32(1)
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "broker",
			Namespace: "some-ns",
		},
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				Size: &replicas,
			},
		},
		Status: brokerv1beta1.ActiveMQArtemisStatus{
			DeploymentPlanSize: replicas,
		},
	}

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	checkOk := func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {
		return nil
	}

	var times = 0
	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			times++
			return apierrors.NewNotFound(schema.GroupResource{}, "")
		},
	}

	client := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	valid := ri.CheckStatus(cr, client, checkOk)
	assert.NotNil(t, valid)
	assert.Contains(t, valid.Error(), "Waiting for")
	assert.Equal(t, times, 1)

	// repeat to verify fake client not called again
	valid = ri.CheckStatus(cr, client, checkOk)
	assert.NotNil(t, valid)
	assert.Contains(t, valid.Error(), "Waiting for")

	assert.Equal(t, times, 1)
}

func TestJolokiaStatusCached(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{Name: "a"},
		Spec:       brokerv1beta1.ActiveMQArtemisSpec{},
	}

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	checkOk := func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {
		return nil
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	a := artemis_client.GetArtemisWithJolokia(j, "a")

	j.EXPECT().
		Read(gomock.Eq("org.apache.activemq.artemis:broker=\"a\"/Status")).
		DoAndReturn(func(_ string) (*jolokia.ResponseData, error) {
			return &jolokia.ResponseData{
				Status:    404,
				Value:     "",
				ErrorType: "javax.management.AttributeNotFoundException",
				Error:     "javax.management.AttributeNotFoundException : No such attribute: Status",
			}, fmt.Errorf("javax.management.AttributeNotFoundException")
		}).Times(1)

	valid := ri.CheckStatusFromJolokia(&jolokia_client.JkInfo{Artemis: a, IP: "IP", Ordinal: "0"}, checkOk)
	assert.NotNil(t, valid)
	assert.True(t, strings.Contains(valid.Error(), "AttributeNotFoundException"))

	// verify status call is cached for second call
	valid = ri.CheckStatusFromJolokia(&jolokia_client.JkInfo{Artemis: a, IP: "IP", Ordinal: "0"}, checkOk)
	assert.NotNil(t, valid)
	assert.True(t, strings.Contains(valid.Error(), "AttributeNotFoundException"))

}

func TestApplyControlPlaneOverrides_NoSecret(t *testing.T) {
	// Test with no override secret - should use defaults
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	scheme := runtime.NewScheme()
	_ = brokerv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	brokerPropertiesMapData := map[string]string{
		"_cert-users":      "default-users",
		"_cert-roles":      "default-roles",
		"login.config":     "default-login",
		"_security.config": "default-security",
	}

	err := applyControlPlaneOverrides(cr, brokerPropertiesMapData, k8sClient, ctrl.Log)

	assert.Nil(t, err)
	// Values should remain unchanged
	assert.Equal(t, "default-users", brokerPropertiesMapData["_cert-users"])
	assert.Equal(t, "default-roles", brokerPropertiesMapData["_cert-roles"])
	assert.Equal(t, "default-login", brokerPropertiesMapData["login.config"])
	assert.Equal(t, "default-security", brokerPropertiesMapData["_security.config"])
}

func TestApplyControlPlaneOverrides_AllKeysOverridden(t *testing.T) {
	// Test with override secret containing all 4 keys
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	overrideSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker-control-plane-override",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"_cert-users":      []byte("custom-users"),
			"_cert-roles":      []byte("custom-roles"),
			"login.config":     []byte("custom-login"),
			"_security.config": []byte("custom-security"),
		},
	}

	scheme := runtime.NewScheme()
	_ = brokerv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overrideSecret).Build()

	brokerPropertiesMapData := map[string]string{
		"_cert-users":      "default-users",
		"_cert-roles":      "default-roles",
		"login.config":     "default-login",
		"_security.config": "default-security",
	}

	err := applyControlPlaneOverrides(cr, brokerPropertiesMapData, k8sClient, ctrl.Log)

	assert.Nil(t, err)
	// All values should be overridden
	assert.Equal(t, "custom-users", brokerPropertiesMapData["_cert-users"])
	assert.Equal(t, "custom-roles", brokerPropertiesMapData["_cert-roles"])
	assert.Equal(t, "custom-login", brokerPropertiesMapData["login.config"])
	assert.Equal(t, "custom-security", brokerPropertiesMapData["_security.config"])
}

func TestApplyControlPlaneOverrides_PartialOverride(t *testing.T) {
	// Test with override secret containing only some keys
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	overrideSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker-control-plane-override",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"_cert-users": []byte("custom-users"),
			"_cert-roles": []byte("custom-roles"),
		},
	}

	scheme := runtime.NewScheme()
	_ = brokerv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overrideSecret).Build()

	brokerPropertiesMapData := map[string]string{
		"_cert-users":      "default-users",
		"_cert-roles":      "default-roles",
		"login.config":     "default-login",
		"_security.config": "default-security",
	}

	err := applyControlPlaneOverrides(cr, brokerPropertiesMapData, k8sClient, ctrl.Log)

	assert.Nil(t, err)
	// Only specified keys should be overridden
	assert.Equal(t, "custom-users", brokerPropertiesMapData["_cert-users"])
	assert.Equal(t, "custom-roles", brokerPropertiesMapData["_cert-roles"])
	assert.Equal(t, "default-login", brokerPropertiesMapData["login.config"])
	assert.Equal(t, "default-security", brokerPropertiesMapData["_security.config"])
}

func TestApplyControlPlaneOverrides_InvalidKeysIgnored(t *testing.T) {
	// Test with override secret containing invalid keys
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	overrideSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker-control-plane-override",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"_cert-users": []byte("custom-users"),
			"invalid-key": []byte("should-be-ignored"),
			"another-bad": []byte("also-ignored"),
		},
	}

	scheme := runtime.NewScheme()
	_ = brokerv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overrideSecret).Build()

	brokerPropertiesMapData := map[string]string{
		"_cert-users":      "default-users",
		"_cert-roles":      "default-roles",
		"login.config":     "default-login",
		"_security.config": "default-security",
	}

	err := applyControlPlaneOverrides(cr, brokerPropertiesMapData, k8sClient, ctrl.Log)

	assert.Nil(t, err)
	// Only valid keys should be overridden, invalid keys ignored
	assert.Equal(t, "custom-users", brokerPropertiesMapData["_cert-users"])
	assert.Equal(t, "default-roles", brokerPropertiesMapData["_cert-roles"])
	assert.Equal(t, "default-login", brokerPropertiesMapData["login.config"])
	assert.Equal(t, "default-security", brokerPropertiesMapData["_security.config"])
	// Invalid keys should not be added to the map
	_, exists := brokerPropertiesMapData["invalid-key"]
	assert.False(t, exists)
	_, exists = brokerPropertiesMapData["another-bad"]
	assert.False(t, exists)
}

func TestApplyControlPlaneOverrides_SecretRetrievalError(t *testing.T) {
	// Test with retrieval error (not NotFound)
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	scheme := runtime.NewScheme()
	_ = brokerv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create client that returns a generic error on Get operations for secrets
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok && strings.Contains(key.Name, "control-plane-override") {
					return fmt.Errorf("simulated retrieval error")
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	brokerPropertiesMapData := map[string]string{
		"_cert-users": "default-users",
	}

	err := applyControlPlaneOverrides(cr, brokerPropertiesMapData, k8sClient, ctrl.Log)

	// Should return an error
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "failed to retrieve control plane override secret"))
}

func TestApplyControlPlaneOverrides_EmptyOverrideSecret(t *testing.T) {
	// Test with override secret that exists but has no valid keys
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	overrideSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker-control-plane-override",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"random-key": []byte("random-value"),
		},
	}

	scheme := runtime.NewScheme()
	_ = brokerv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overrideSecret).Build()

	brokerPropertiesMapData := map[string]string{
		"_cert-users":      "default-users",
		"_cert-roles":      "default-roles",
		"login.config":     "default-login",
		"_security.config": "default-security",
	}

	err := applyControlPlaneOverrides(cr, brokerPropertiesMapData, k8sClient, ctrl.Log)

	assert.Nil(t, err)
	// All default values should remain unchanged
	assert.Equal(t, "default-users", brokerPropertiesMapData["_cert-users"])
	assert.Equal(t, "default-roles", brokerPropertiesMapData["_cert-roles"])
	assert.Equal(t, "default-login", brokerPropertiesMapData["login.config"])
	assert.Equal(t, "default-security", brokerPropertiesMapData["_security.config"])
}
