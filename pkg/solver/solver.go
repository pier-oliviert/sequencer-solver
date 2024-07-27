package solver

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	eDNS "github.com/pier-oliviert/external-dns/endpoint"
	sequencer "github.com/pier-oliviert/sequencer/api/v1alpha1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func New(name, namespace string) webhook.Solver {
	return &SolverProvider{name: name, namespace: namespace}
}

type SolverProvider struct {
	name      string
	namespace string
	client    rest.Interface
}

func (sp *SolverProvider) Initialize(c *rest.Config, stopCh <-chan struct{}) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	config.ContentConfig.GroupVersion = &sequencer.GroupVersion
	config.APIPath = "/apis"

	client, err := rest.RESTClientFor(config)
	if err != nil {
		return err
	}

	scheme.Scheme.AddKnownTypes(sequencer.GroupVersion,
		&eDNS.DNSEndpoint{},
		&eDNS.DNSEndpointList{},
	)
	meta.AddToGroupVersion(scheme.Scheme, sequencer.GroupVersion)
	sp.client = client

	return nil
}

func (sp *SolverProvider) Name() string {
	return sp.name
}

func (sp *SolverProvider) Present(ch *whapi.ChallengeRequest) error {

	ctx := context.Background()
	// Create a DNSEndpoint
	ep := &eDNS.DNSEndpoint{
		ObjectMeta: meta.ObjectMeta{
			Namespace:    sp.namespace,
			GenerateName: "dns01-challenge-",
			Labels: map[string]string{
				"solver.se.quencer.io/dns-name": strings.TrimPrefix(strings.TrimSuffix(ch.DNSName, "."), "_"),
				"solver.se.quencer.io/fqdn":     strings.TrimPrefix(strings.TrimSuffix(ch.ResolvedFQDN, "."), "_"),
				"solver.se.quencer.io/zone":     strings.TrimPrefix(strings.TrimSuffix(ch.ResolvedZone, "."), "_"),
			},
		},
		Spec: eDNS.DNSEndpointSpec{
			Endpoints: []*eDNS.Endpoint{{
				RecordType: eDNS.RecordTypeTXT,
				DNSName:    strings.TrimSuffix(ch.ResolvedFQDN, "."),
				Targets:    eDNS.NewTargets(ch.Key),
			}},
		},
	}
	result := sp.client.Post().Namespace(sp.namespace).Resource("dnsendpoints").Body(ep).Do(ctx)

	return result.Error()
}

func (sp *SolverProvider) CleanUp(ch *whapi.ChallengeRequest) error {
	ctx := context.Background()
	var challenges eDNS.DNSEndpointList
	opts := meta.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "solver.se.quencer.io/fqdn", strings.TrimPrefix(strings.TrimSuffix(ch.ResolvedFQDN, "."), "_")),
	}

	// Populate the labels for each record with the RegistryEntry matching.
	err := sp.client.Get().Namespace(sp.namespace).Resource("dnsendpoints").VersionedParams(&opts, scheme.ParameterCodec).Do(ctx).Into(&challenges)
	if err != nil {
		return err
	}

	// Delete only the DNSEndpoint that has the same key. It's unlikely there's more than one, but since
	// it's a list, let's process all of them.

	for _, c := range challenges.Items {
		for _, ep := range c.Spec.Endpoints {
			if ep.Targets[0] == ch.Key {
				result := sp.client.Delete().Namespace(sp.namespace).Resource("dnsendpoints").Name(c.Name).Do(ctx)
				if err := result.Error(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
