// Package attributes adapts IP intelligence services to the same typed result.
// Add a Provider and register it here to support another response schema.
package attributes

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/Resinat/Resin/internal/node"
)

type Provider interface {
	ID() string
	Name() string
	DefaultURL() string
	Parse([]byte) (*node.EgressAttributes, error)
}

var providers = map[string]Provider{"check-host": CheckHost{}}

type Descriptor struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DefaultURL string `json:"default_url"`
}

func List() []Descriptor {
	result := make([]Descriptor, 0, len(providers))
	for _, provider := range providers {
		result = append(result, Descriptor{provider.ID(), provider.Name(), provider.DefaultURL()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func Lookup(id string) (Provider, bool) { provider, ok := providers[id]; return provider, ok }

// CheckHost implements the same source and classification rules as Easy Test.
type CheckHost struct{}

func (CheckHost) ID() string         { return "check-host" }
func (CheckHost) Name() string       { return "Check Host" }
func (CheckHost) DefaultURL() string { return "https://api.check-host.cc/myinfo" }

func (CheckHost) Parse(body []byte) (*node.EgressAttributes, error) {
	var response struct {
		Success     bool   `json:"success"`
		IP          string `json:"ip"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		City        string `json:"city"`
		ASN         struct {
			Number json.RawMessage `json:"asn"`
			Name   string          `json:"name"`
			Type   string          `json:"type"`
		} `json:"asn"`
		Company struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"company"`
		Privacy struct {
			Hosting *bool `json:"isHosting"`
			Proxy   *bool `json:"isProxy"`
			VPN     *bool `json:"isVpn"`
			Tor     *bool `json:"isTor"`
		} `json:"privacy"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("check-host response: %w", err)
	}
	if !response.Success {
		return nil, fmt.Errorf("check-host returned an unsuccessful response")
	}
	ip, err := netip.ParseAddr(response.IP)
	if err != nil {
		return nil, fmt.Errorf("check-host returned an invalid exit IP")
	}
	asn := strings.Trim(string(response.ASN.Number), "\"")
	if asn == "null" {
		asn = ""
	}
	company := response.Company.Name
	if company == "" {
		company = response.ASN.Name
	}
	kind := strings.ToLower(strings.TrimSpace(response.Company.Type))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(response.ASN.Type))
	}
	if response.Privacy.Hosting != nil && *response.Privacy.Hosting && kind != "hosting" {
		if kind == "" {
			kind = "hosting"
		} else {
			kind += " / hosting"
		}
	}
	if kind == "" {
		kind = "unknown"
	}
	return &node.EgressAttributes{
		IP: ip.String(), CountryCode: strings.ToUpper(strings.TrimSpace(response.CountryCode)), Country: response.Country, City: response.City,
		ASN: asn, Organization: response.ASN.Name, Company: company, CompanyType: strings.ToLower(response.Company.Type), ASNType: strings.ToLower(response.ASN.Type),
		NetworkType: kind, Hosting: response.Privacy.Hosting, IsProxy: response.Privacy.Proxy, IsVPN: response.Privacy.VPN, IsTor: response.Privacy.Tor,
	}, nil
}
