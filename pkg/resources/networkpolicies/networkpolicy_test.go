package networkpolicies

import (
	"testing"

	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
)

func TestNewNetworkPolicy_DefaultPorts(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "my-broker", "test-ns", false, nil)

	assert.Equal(t, "my-broker-netpol", netpol.Name)
	assert.Equal(t, "test-ns", netpol.Namespace)
	assert.Equal(t, "NetworkPolicy", netpol.TypeMeta.Kind)
	assert.Equal(t, "networking.k8s.io/v1", netpol.TypeMeta.APIVersion)

	assert.Equal(t, "my-broker", netpol.Spec.PodSelector.MatchLabels["ActiveMQArtemis"])
	assert.Contains(t, netpol.Spec.PolicyTypes, netv1.PolicyTypeIngress)
	assert.Contains(t, netpol.Spec.PolicyTypes, netv1.PolicyTypeEgress)

	ports := collectPorts(netpol)
	assert.Contains(t, ports, int32(7800))
	assert.Contains(t, ports, int32(8161))
	assert.Contains(t, ports, int32(8778))
	assert.Contains(t, ports, int32(61616))
}

func TestNewNetworkPolicy_EgressAllowAll(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", false, nil)

	assert.Len(t, netpol.Spec.Egress, 1, "one egress rule")
	assert.Empty(t, netpol.Spec.Egress[0].Ports, "empty rule = allow all")
	assert.Empty(t, netpol.Spec.Egress[0].To, "no destination restriction")
}

func TestNewNetworkPolicy_WithAcceptorPorts(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", false, []int32{5672, 1883})
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(5672))
	assert.Contains(t, ports, int32(1883))
	assert.Contains(t, ports, int32(61616), "default port still present")
}

func TestNewNetworkPolicy_DeduplicatesPorts(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", false, []int32{61616})
	ports := collectPorts(netpol)

	count := 0
	for _, p := range ports {
		if p == 61616 {
			count++
		}
	}
	assert.Equal(t, 1, count, "port 61616 should appear exactly once")
}

func TestNewNetworkPolicy_Restricted(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", true, nil)
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(8778), "jolokia agent")
	assert.Contains(t, ports, int32(8888), "prometheus agent")
	assert.NotContains(t, ports, int32(7800), "no jgroups in restricted")
	assert.NotContains(t, ports, int32(8161), "no console in restricted")
	assert.NotContains(t, ports, int32(61616), "no all-protocols in restricted")
	assert.Len(t, ports, 2)
}

func TestNewNetworkPolicy_RestrictedIgnoresAcceptorPorts(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", true, []int32{5672})
	ports := collectPorts(netpol)

	assert.NotContains(t, ports, int32(5672), "acceptor ports ignored in restricted mode")
	assert.Contains(t, ports, int32(8778))
	assert.Contains(t, ports, int32(8888))
}

func TestNewNetworkPolicy_ReusesExisting(t *testing.T) {
	existing := &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "broker-netpol",
			Namespace:       "ns",
			ResourceVersion: "12345",
		},
	}

	netpol := NewNetworkPolicy(existing, "broker", "ns", false, nil)

	assert.Equal(t, "12345", netpol.ResourceVersion, "should preserve existing resource version")
	assert.Equal(t, "broker", netpol.Spec.PodSelector.MatchLabels["ActiveMQArtemis"])
}

func TestNewNetworkPolicyFromSpec_UsesProvidedSpec(t *testing.T) {
	spec := &netv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{"ActiveMQArtemis": "my-broker"},
		},
		PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
	}

	netpol := NewNetworkPolicyFromSpec(nil, "my-broker", "ns", spec)

	assert.Equal(t, "my-broker-netpol", netpol.Name)
	assert.Equal(t, "ns", netpol.Namespace)
	assert.Equal(t, spec.PodSelector, netpol.Spec.PodSelector)
}

func TestNewNetworkPolicyFromSpec_PreservesExistingMetadata(t *testing.T) {
	existing := &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-broker-netpol",
			Namespace:       "ns",
			ResourceVersion: "99999",
		},
	}

	spec := &netv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{"ActiveMQArtemis": "my-broker"},
		},
		PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
	}

	netpol := NewNetworkPolicyFromSpec(existing, "my-broker", "ns", spec)

	assert.Equal(t, "99999", netpol.ResourceVersion, "preserves resource version")
}

func collectPorts(netpol *netv1.NetworkPolicy) []int32 {
	var ports []int32
	for _, rule := range netpol.Spec.Ingress {
		for _, p := range rule.Ports {
			if p.Port != nil {
				ports = append(ports, int32(p.Port.IntValue()))
			}
		}
	}
	return ports
}
