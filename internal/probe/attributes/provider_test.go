package attributes

import "testing"

func TestCheckHost_EducationFallbackAndHostingUnknown(t *testing.T) {
	attributes, err := (CheckHost{}).Parse([]byte(`{"success":true,"ip":"203.0.113.1","countryCode":"us","asn":{"asn":"AS64500","name":"University Network","type":"education"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if attributes.Company != "University Network" || attributes.NetworkType != "education" || attributes.CountryCode != "US" || attributes.Hosting != nil {
		t.Fatalf("incorrect fallback or unknown flags: %+v", attributes)
	}
}

func TestCheckHost_PreservesConflictingTypes(t *testing.T) {
	attributes, err := (CheckHost{}).Parse([]byte(`{"success":true,"ip":"203.0.113.1","company":{"name":"Example Cloud","type":"isp"},"asn":{"type":"education"},"privacy":{"isHosting":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if attributes.NetworkType != "isp / hosting" || !attributes.HasNetworkType("education") || !attributes.HasNetworkType("hosting") {
		t.Fatalf("lost source classification: %+v", attributes)
	}
	for _, body := range []string{`{"success":false}`, `{"success":true,"ip":"not-an-ip"}`} {
		if _, err := (CheckHost{}).Parse([]byte(body)); err == nil {
			t.Fatalf("invalid response accepted: %s", body)
		}
	}
}
