package provider

import "github.com/emergent-company/emergent.memory/pkg/adk"

// ProtocolForBaseURL returns the effective wire protocol for this vendor at the
// given base URL. It is the data-driven Responses selection point: a vendor
// whose default protocol is openai-chat and which declares ResponsesHosts is
// promoted to ProtocolOpenAIResponses when the resolved base URL's host matches
// one of those hosts. Any other host — a relay, proxy, or local server — keeps
// the default protocol, so proxied OpenAI is never silently switched.
//
// A nil definition returns "".
func (d *ProviderDefinition) ProtocolForBaseURL(baseURL string) Protocol {
	if d == nil {
		return ""
	}
	if d.Protocol == ProtocolOpenAIChat && len(d.ResponsesHosts) > 0 &&
		adk.HostMatchesAny(baseURL, d.ResponsesHosts) {
		return ProtocolOpenAIResponses
	}
	return d.Protocol
}
